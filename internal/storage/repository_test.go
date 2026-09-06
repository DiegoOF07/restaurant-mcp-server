package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
)

// Las dos implementaciones de Repository se prueban con EL MISMO conjunto de casos.
// Es lo que hace que las pruebas de las herramientas, que corren contra la versión en
// memoria por rapidez, sigan diciendo algo cierto sobre la de SQLite que corre en producción.
func eachRepository(t *testing.T, run func(t *testing.T, repo storage.Repository)) {
	t.Helper()

	t.Run("memoria", func(t *testing.T) {
		repo := storage.NewInMemoryRepository()
		repo.Seed()
		run(t, repo)
	})

	t.Run("sqlite", func(t *testing.T) {
		run(t, newSeededSQLite(t, storage.MemoryDSN))
	})
}

func newSeededSQLite(t *testing.T, path string) *storage.SQLiteRepository {
	t.Helper()

	repo, err := storage.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite(%q): %v", path, err)
	}
	t.Cleanup(func() { repo.Close() })

	if err := repo.Seed(); err != nil {
		t.Fatalf("Seed(): %v", err)
	}
	return repo
}

func TestRepository_SeedLoadsCatalog(t *testing.T) {
	eachRepository(t, func(t *testing.T, repo storage.Repository) {
		dish, ok := repo.FindDish("special-burger")
		if !ok {
			t.Fatal("no se encontró el platillo sembrado")
		}
		if dish.Name != "Hamburguesa Especial" || !dish.Active {
			t.Errorf("platillo mal cargado: %+v", dish)
		}

		items, ok := repo.RecipeForDish("special-burger")
		if !ok || len(items) != 4 {
			t.Fatalf("receta esperada de 4 ingredientes, se obtuvo %d (ok=%t)", len(items), ok)
		}

		qty, ok := repo.InventoryQuantity("cheese")
		if !ok || qty != 40 {
			t.Errorf("existencia de queso = %d (ok=%t), se esperaba 40", qty, ok)
		}
	})
}

func TestRepository_SearchIngredientsIsOrderedAndMatchesIDs(t *testing.T) {
	eachRepository(t, func(t *testing.T, repo storage.Repository) {
		all := repo.SearchIngredients("")
		if len(all) != 8 {
			t.Fatalf("se esperaban 8 ingredientes, se obtuvieron %d", len(all))
		}
		for i := 1; i < len(all); i++ {
			if all[i-1].ID > all[i].ID {
				t.Fatalf("resultados sin ordenar: %q antes de %q", all[i-1].ID, all[i].ID)
			}
		}

		// Buscar por nombre y por identificador debe encontrar lo mismo.
		byName := repo.SearchIngredients("Queso")
		byID := repo.SearchIngredients("cheese")
		if len(byName) != 1 || len(byID) != 1 || byName[0].ID != byID[0].ID {
			t.Fatalf("búsqueda por nombre=%+v y por id=%+v deberían coincidir", byName, byID)
		}
		if byName[0].AllergenCategory != "lácteos" || byName[0].BaseUnit != domain.UnitGram {
			t.Errorf("ingrediente mal cargado: %+v", byName[0])
		}
	})
}

func TestRepository_ApplyMovementSubtractsAndAudits(t *testing.T) {
	eachRepository(t, func(t *testing.T, repo storage.Repository) {
		result, err := repo.ApplyMovement(storage.MovementRequest{
			IdempotencyKey: "k1",
			IngredientID:   "cheese",
			Operation:      "subtract",
			QuantityBase:   10,
			Reason:         "damaged",
			PerformedBy:    "diego",
		})
		if err != nil {
			t.Fatalf("ApplyMovement: %v", err)
		}
		if result.Idempotent {
			t.Error("el primer movimiento no debería marcarse como idempotente")
		}
		if result.Movement.PreviousQuantity != 40 || result.Movement.ResultingQuantity != 30 {
			t.Errorf("cantidades mal registradas: %+v", result.Movement)
		}
		if result.Movement.PerformedBy != "diego" {
			t.Errorf("PerformedBy = %q, se esperaba %q", result.Movement.PerformedBy, "diego")
		}
		if result.Movement.ID == "" {
			t.Error("el movimiento debe recibir un identificador")
		}

		if qty, _ := repo.InventoryQuantity("cheese"); qty != 30 {
			t.Errorf("existencia tras el ajuste = %d, se esperaba 30", qty)
		}
	})
}

func TestRepository_ApplyMovementIsIdempotent(t *testing.T) {
	eachRepository(t, func(t *testing.T, repo storage.Repository) {
		req := storage.MovementRequest{
			IdempotencyKey: "misma-clave",
			IngredientID:   "cheese",
			Operation:      "subtract",
			QuantityBase:   10,
		}

		first, err := repo.ApplyMovement(req)
		if err != nil {
			t.Fatalf("primer intento: %v", err)
		}

		second, err := repo.ApplyMovement(req)
		if err != nil {
			t.Fatalf("reintento: %v", err)
		}
		if !second.Idempotent {
			t.Error("el reintento debería marcarse como idempotente")
		}
		if second.Movement.ID != first.Movement.ID {
			t.Errorf("el reintento devolvió otro movimiento: %q vs %q", second.Movement.ID, first.Movement.ID)
		}

		// Lo que realmente importa: no se descontó dos veces.
		if qty, _ := repo.InventoryQuantity("cheese"); qty != 30 {
			t.Errorf("existencia = %d, se esperaba 30 (el reintento no debe volver a descontar)", qty)
		}
	})
}

func TestRepository_ApplyMovementRejectsNegativeInventory(t *testing.T) {
	eachRepository(t, func(t *testing.T, repo storage.Repository) {
		_, err := repo.ApplyMovement(storage.MovementRequest{
			IdempotencyKey: "demasiado",
			IngredientID:   "cheese",
			Operation:      "subtract",
			QuantityBase:   1000, // sólo hay 40
		})
		if err != domain.ErrInsufficientInventory {
			t.Fatalf("error = %v, se esperaba ErrInsufficientInventory", err)
		}

		// El rechazo no debe dejar rastro: ni existencia alterada ni movimiento a medias.
		if qty, _ := repo.InventoryQuantity("cheese"); qty != 40 {
			t.Errorf("existencia = %d, se esperaba 40 intacta tras el rechazo", qty)
		}
	})
}

func TestRepository_ApplyMovementRejectsUnknownIngredient(t *testing.T) {
	eachRepository(t, func(t *testing.T, repo storage.Repository) {
		_, err := repo.ApplyMovement(storage.MovementRequest{
			IdempotencyKey: "fantasma",
			IngredientID:   "no-existe",
			Operation:      "add",
			QuantityBase:   5,
		})
		if err != domain.ErrIngredientNotFound {
			t.Fatalf("error = %v, se esperaba ErrIngredientNotFound", err)
		}
	})
}

func TestRepository_ApplyMovementRejectsUnknownOperation(t *testing.T) {
	eachRepository(t, func(t *testing.T, repo storage.Repository) {
		_, err := repo.ApplyMovement(storage.MovementRequest{
			IdempotencyKey: "rara",
			IngredientID:   "cheese",
			Operation:      "multiply",
			QuantityBase:   2,
		})
		if err != domain.ErrInvalidOperation {
			t.Fatalf("error = %v, se esperaba ErrInvalidOperation", err)
		}
	})
}

// Esta es la razón de ser de SQLite: que el inventario siga ahí después de cerrar el
// servidor. Es lo único que la versión en memoria NO puede cumplir, así que se prueba aparte.
func TestSQLite_PersistsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restaurant.db")

	first := newSeededSQLite(t, path)
	if _, err := first.ApplyMovement(storage.MovementRequest{
		IdempotencyKey: "cierre-de-turno",
		IngredientID:   "cheese",
		Operation:      "subtract",
		QuantityBase:   10,
		Reason:         "damaged",
		PerformedBy:    "diego",
	}); err != nil {
		t.Fatalf("ApplyMovement: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Segundo arranque sobre el mismo archivo, como haría el host al relanzar el servidor.
	second, err := storage.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reapertura: %v", err)
	}
	t.Cleanup(func() { second.Close() })

	if empty, err := second.IsEmpty(); err != nil || empty {
		t.Fatalf("IsEmpty = %t (err=%v); una base ya sembrada no debe reportarse vacía", empty, err)
	}
	if qty, ok := second.InventoryQuantity("cheese"); !ok || qty != 30 {
		t.Errorf("existencia tras reiniciar = %d (ok=%t), se esperaba 30", qty, ok)
	}

	// La clave de idempotencia también sobrevive: un reintento tras reiniciar
	// tampoco puede volver a descontar.
	movement, ok := second.MovementByKey("cierre-de-turno")
	if !ok {
		t.Fatal("el movimiento no sobrevivió al reinicio")
	}
	if movement.PerformedBy != "diego" || movement.Reason != "damaged" {
		t.Errorf("auditoría incompleta tras reiniciar: %+v", movement)
	}
	if movement.CreatedAt.IsZero() {
		t.Error("la marca de tiempo no sobrevivió al reinicio")
	}
}

// Resembrar una base que ya se usó debe restaurar el catálogo sin pisar el inventario
// ajustado: es la diferencia entre "recargar el menú" y "borrar el trabajo del turno".
func TestSQLite_ReseedPreservesAdjustedInventory(t *testing.T) {
	repo := newSeededSQLite(t, storage.MemoryDSN)

	if _, err := repo.ApplyMovement(storage.MovementRequest{
		IdempotencyKey: "ajuste",
		IngredientID:   "cheese",
		Operation:      "subtract",
		QuantityBase:   10,
	}); err != nil {
		t.Fatalf("ApplyMovement: %v", err)
	}

	if err := repo.Seed(); err != nil {
		t.Fatalf("resiembra: %v", err)
	}

	if qty, _ := repo.InventoryQuantity("cheese"); qty != 30 {
		t.Errorf("existencia tras resembrar = %d, se esperaba 30 (no debe volver a 40)", qty)
	}
	if _, ok := repo.FindDish("special-burger"); !ok {
		t.Error("la resiembra debe conservar el catálogo")
	}
}

func TestSQLite_AddDish(t *testing.T) {
	repo := newSeededSQLite(t, storage.MemoryDSN)

	if err := repo.AddDish(domain.Dish{ID: "salad", Name: "Ensalada César", Active: true}); err != nil {
		t.Fatalf("AddDish: %v", err)
	}
	found := repo.SearchDishes("ensalada")
	if len(found) != 1 || found[0].ID != "salad" {
		t.Fatalf("SearchDishes no encontró el platillo agregado: %+v", found)
	}
}
