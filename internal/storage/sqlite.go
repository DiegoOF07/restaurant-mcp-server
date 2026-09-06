package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"

	_ "modernc.org/sqlite"
)

// MemoryDSN abre una base efímera en RAM.
const MemoryDSN = ":memory:"

// schema es idempotente: se ejecuta en cada arranque y sólo crea lo que falte.
//
// Las dos invariantes críticas del dominio viven acá como restricciones, no sólo como
// código Go: una base corrupta deja de ser posible aunque un futuro handler se olvide de
// validar.
//
//	UNIQUE(idempotency_key)     -> un reintento no puede descontar dos veces.
//	CHECK(quantity_base >= 0)   -> el inventario no puede quedar negativo.
const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS ingredients (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    base_unit         TEXT NOT NULL,
    allergen_category TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS dishes (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    active      INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS recipe_items (
    dish_id              TEXT NOT NULL REFERENCES dishes(id) ON DELETE CASCADE,
    ingredient_id        TEXT NOT NULL REFERENCES ingredients(id),
    quantity_per_serving INTEGER NOT NULL CHECK (quantity_per_serving > 0),
    position             INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (dish_id, ingredient_id)
);

CREATE TABLE IF NOT EXISTS inventory (
    ingredient_id TEXT PRIMARY KEY REFERENCES ingredients(id),
    quantity_base INTEGER NOT NULL CHECK (quantity_base >= 0)
);

CREATE TABLE IF NOT EXISTS inventory_movements (
    seq                INTEGER PRIMARY KEY AUTOINCREMENT,
    id                 TEXT GENERATED ALWAYS AS (printf('mv-%06d', seq)) VIRTUAL,
    idempotency_key    TEXT NOT NULL UNIQUE,
    ingredient_id      TEXT NOT NULL REFERENCES ingredients(id),
    operation          TEXT NOT NULL CHECK (operation IN ('add', 'subtract', 'set')),
    quantity_base      INTEGER NOT NULL,
    reason             TEXT NOT NULL DEFAULT '',
    previous_quantity  INTEGER NOT NULL,
    resulting_quantity INTEGER NOT NULL CHECK (resulting_quantity >= 0),
    performed_by       TEXT NOT NULL DEFAULT 'unspecified',
    created_at         TEXT NOT NULL
);
`

// Ambas implementaciones deben satisfacer el mismo contrato; si una se desincroniza,
// esto falla al compilar en vez de en tiempo de ejecución.
var (
	_ Repository = (*InMemoryRepository)(nil)
	_ Repository = (*SQLiteRepository)(nil)
)

// SQLiteRepository es la implementación persistente de Repository.
type SQLiteRepository struct {
	db *sql.DB
}

// OpenSQLite abre la base en path y aplica el esquema.
// Usa MemoryDSN para una base en RAM.
func OpenSQLite(path string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir la base %q: %w", path, err)
	}

	// Un solo escritor. El servidor stdio atiende un cliente a la vez, así que serializar
	// el acceso cuesta nada y elimina de raíz los SQLITE_BUSY entre conexiones del pool.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("no se pudo conectar a la base %q: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("no se pudo aplicar el esquema en %q: %w", path, err)
	}

	return &SQLiteRepository{db: db}, nil
}

// dsnFor arma la cadena de conexión. Los PRAGMA van en la URL porque deben aplicarse a
// CADA conexión del pool: ejecutarlos una vez con Exec sólo afectaría a una.
func dsnFor(path string) string {
	if path == MemoryDSN || strings.HasPrefix(path, "file::memory:") {
		return "file::memory:?_pragma=foreign_keys(1)&_txlock=immediate"
	}

	return "file:" + url.PathEscape(path) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"
}

// Close libera la base. El llamador debe invocarlo al terminar
func (r *SQLiteRepository) Close() error { return r.db.Close() }

// IsEmpty indica si la base todavía no tiene catálogo, para decidir si sembrarla
func (r *SQLiteRepository) IsEmpty() (bool, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM ingredients`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// Seed carga el catálogo de demostración. Es idempotente (upsert), así que volver a
// llamarlo restaura el catálogo sin duplicar filas ni tocar los movimientos ya registrados.
func (r *SQLiteRepository) Seed() error {
	data := DemoSeed()

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, ing := range data.Ingredients {
		if _, err := tx.Exec(
			`INSERT INTO ingredients (id, name, base_unit, allergen_category) VALUES (?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name = excluded.name, base_unit = excluded.base_unit,
			                               allergen_category = excluded.allergen_category`,
			ing.ID, ing.Name, string(ing.BaseUnit), ing.AllergenCategory,
		); err != nil {
			return fmt.Errorf("sembrando ingrediente %q: %w", ing.ID, err)
		}
	}

	for _, d := range data.Dishes {
		if _, err := tx.Exec(
			`INSERT INTO dishes (id, name, description, active) VALUES (?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name = excluded.name, description = excluded.description,
			                               active = excluded.active`,
			d.ID, d.Name, d.Description, boolToInt(d.Active),
		); err != nil {
			return fmt.Errorf("sembrando platillo %q: %w", d.ID, err)
		}
	}

	for _, dish := range data.Dishes {
		for position, item := range data.Recipes[dish.ID] {
			if _, err := tx.Exec(
				`INSERT INTO recipe_items (dish_id, ingredient_id, quantity_per_serving, position)
				 VALUES (?, ?, ?, ?)
				 ON CONFLICT(dish_id, ingredient_id) DO UPDATE SET
				     quantity_per_serving = excluded.quantity_per_serving, position = excluded.position`,
				item.DishID, item.IngredientID, item.QuantityPerServing, position,
			); err != nil {
				return fmt.Errorf("sembrando receta de %q: %w", dish.ID, err)
			}
		}
	}

	// El inventario sólo se inicializa si el ingrediente aún no tiene existencia registrada
	for ingID, qty := range data.Inventory {
		if _, err := tx.Exec(
			`INSERT INTO inventory (ingredient_id, quantity_base) VALUES (?, ?)
			 ON CONFLICT(ingredient_id) DO NOTHING`,
			ingID, qty,
		); err != nil {
			return fmt.Errorf("sembrando inventario de %q: %w", ingID, err)
		}
	}

	return tx.Commit()
}

// AddDish registra o reemplaza un platillo
func (r *SQLiteRepository) AddDish(d domain.Dish) error {
	_, err := r.db.Exec(
		`INSERT INTO dishes (id, name, description, active) VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name = excluded.name, description = excluded.description,
		                               active = excluded.active`,
		d.ID, d.Name, d.Description, boolToInt(d.Active),
	)
	return err
}

// FindDish busca un platillo por su identificador exacto.
func (r *SQLiteRepository) FindDish(id string) (domain.Dish, bool) {
	var d domain.Dish
	var active int
	err := r.db.QueryRow(
		`SELECT id, name, description, active FROM dishes WHERE id = ?`, id,
	).Scan(&d.ID, &d.Name, &d.Description, &active)
	if err != nil {
		return domain.Dish{}, false
	}
	d.Active = active != 0
	return d, true
}

// SearchDishes devuelve los platillos cuyo nombre contenga query, sin distinguir
// mayúsculas, ordenados por identificador para que la salida sea estable.
func (r *SQLiteRepository) SearchDishes(query string) []domain.Dish {
	q := strings.ToLower(strings.TrimSpace(query))
	rows, err := r.db.Query(
		`SELECT id, name, description, active FROM dishes
		 WHERE ? = '' OR instr(lower(name), ?) > 0
		 ORDER BY id`, q, q,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []domain.Dish
	for rows.Next() {
		var d domain.Dish
		var active int
		if err := rows.Scan(&d.ID, &d.Name, &d.Description, &active); err != nil {
			return results
		}
		d.Active = active != 0
		results = append(results, d)
	}
	return results
}

// RecipeForDish devuelve los ingredientes de un platillo en el orden en que se
// cargaron. El booleano distingue "el platillo no existe" de "existe pero no
// tiene receta cargada".
func (r *SQLiteRepository) RecipeForDish(dishID string) ([]domain.RecipeItem, bool) {
	if _, ok := r.FindDish(dishID); !ok {
		return nil, false
	}

	rows, err := r.db.Query(
		`SELECT dish_id, ingredient_id, quantity_per_serving FROM recipe_items
		 WHERE dish_id = ? ORDER BY position, ingredient_id`, dishID,
	)
	if err != nil {
		return nil, true
	}
	defer rows.Close()

	var items []domain.RecipeItem
	for rows.Next() {
		var item domain.RecipeItem
		if err := rows.Scan(&item.DishID, &item.IngredientID, &item.QuantityPerServing); err != nil {
			return items, true
		}
		items = append(items, item)
	}
	return items, true
}

// Ingredient busca un ingrediente por su identificador exacto.
func (r *SQLiteRepository) Ingredient(id string) (domain.Ingredient, bool) {
	var ing domain.Ingredient
	var baseUnit string
	err := r.db.QueryRow(
		`SELECT id, name, base_unit, allergen_category FROM ingredients WHERE id = ?`, id,
	).Scan(&ing.ID, &ing.Name, &baseUnit, &ing.AllergenCategory)
	if err != nil {
		return domain.Ingredient{}, false
	}
	ing.BaseUnit = domain.Unit(baseUnit)
	return ing, true
}

// SearchIngredients busca por nombre o por identificador, sin distinguir mayúsculas,
// ordenando por identificador. Un query vacío devuelve todos.
func (r *SQLiteRepository) SearchIngredients(query string) []domain.Ingredient {
	q := strings.ToLower(strings.TrimSpace(query))
	rows, err := r.db.Query(
		`SELECT id, name, base_unit, allergen_category FROM ingredients
		 WHERE ? = '' OR instr(lower(name), ?) > 0 OR instr(lower(id), ?) > 0
		 ORDER BY id`, q, q, q,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	results := make([]domain.Ingredient, 0)
	for rows.Next() {
		var ing domain.Ingredient
		var baseUnit string
		if err := rows.Scan(&ing.ID, &ing.Name, &baseUnit, &ing.AllergenCategory); err != nil {
			return results
		}
		ing.BaseUnit = domain.Unit(baseUnit)
		results = append(results, ing)
	}
	return results
}

// InventoryQuantity devuelve la existencia actual en la unidad base del ingrediente.
// El booleano indica si el ingrediente existe, no si tiene existencia: un ingrediente
// sembrado pero sin fila de inventario devuelve (0, true).
func (r *SQLiteRepository) InventoryQuantity(ingredientID string) (int64, bool) {
	if _, ok := r.Ingredient(ingredientID); !ok {
		return 0, false
	}

	var qty int64
	err := r.db.QueryRow(
		`SELECT quantity_base FROM inventory WHERE ingredient_id = ?`, ingredientID,
	).Scan(&qty)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, true // el ingrediente existe pero nunca se le registró existencia
	}
	if err != nil {
		return 0, false
	}
	return qty, true
}

// MovementByKey devuelve el movimiento registrado bajo una clave de idempotencia.
// Sirve para auditar quién hizo cada ajuste y para verificar que un reintento no volvió a descontar
func (r *SQLiteRepository) MovementByKey(key string) (domain.InventoryMovement, bool) {
	return scanMovement(r.db.QueryRow(movementSelect+` WHERE idempotency_key = ?`, key))
}

const movementSelect = `
	SELECT id, idempotency_key, ingredient_id, operation, quantity_base, reason,
	       previous_quantity, resulting_quantity, performed_by, created_at
	FROM inventory_movements`

type rowScanner interface{ Scan(dest ...any) error }

func scanMovement(row rowScanner) (domain.InventoryMovement, bool) {
	var m domain.InventoryMovement
	var createdAt string
	err := row.Scan(&m.ID, &m.IdempotencyKey, &m.IngredientID, &m.Operation, &m.QuantityBase,
		&m.Reason, &m.PreviousQuantity, &m.ResultingQuantity, &m.PerformedBy, &createdAt)
	if err != nil {
		return domain.InventoryMovement{}, false
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return m, true
}

// ApplyMovement aplica un ajuste de inventario dentro de una transacción. Si algo falla a
// mitad, la base queda exactamente como estaba.
func (r *SQLiteRepository) ApplyMovement(req MovementRequest) (MovementResult, error) {
	// El DSN abre las transacciones en modo IMMEDIATE (ver dsnFor).
	tx, err := r.db.Begin()
	if err != nil {
		return MovementResult{}, err
	}
	defer tx.Rollback()

	if existing, ok := scanMovement(
		tx.QueryRow(movementSelect+` WHERE idempotency_key = ?`, req.IdempotencyKey),
	); ok {
		return MovementResult{Movement: existing, Idempotent: true}, nil
	}

	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM ingredients WHERE id = ?`, req.IngredientID).Scan(&exists); err != nil {
		return MovementResult{}, err
	}
	if exists == 0 {
		return MovementResult{}, domain.ErrIngredientNotFound
	}

	var previous int64
	err = tx.QueryRow(`SELECT quantity_base FROM inventory WHERE ingredient_id = ?`, req.IngredientID).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return MovementResult{}, err
	}

	var resulting int64
	switch req.Operation {
	case "add":
		resulting = previous + req.QuantityBase
	case "subtract":
		resulting = previous - req.QuantityBase
	case "set":
		resulting = req.QuantityBase
	default:
		return MovementResult{}, domain.ErrInvalidOperation
	}

	// Se comprueba acá para poder devolver un error de dominio legible
	if resulting < 0 {
		return MovementResult{}, domain.ErrInsufficientInventory
	}

	createdAt := time.Now().UTC()
	res, err := tx.Exec(
		`INSERT INTO inventory_movements
		     (idempotency_key, ingredient_id, operation, quantity_base, reason,
		      previous_quantity, resulting_quantity, performed_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.IdempotencyKey, req.IngredientID, req.Operation, req.QuantityBase, req.Reason,
		previous, resulting, performerOrDefault(req.PerformedBy), createdAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return MovementResult{}, err
	}

	if _, err := tx.Exec(
		`INSERT INTO inventory (ingredient_id, quantity_base) VALUES (?, ?)
		 ON CONFLICT(ingredient_id) DO UPDATE SET quantity_base = excluded.quantity_base`,
		req.IngredientID, resulting,
	); err != nil {
		return MovementResult{}, err
	}

	seq, err := res.LastInsertId()
	if err != nil {
		return MovementResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return MovementResult{}, err
	}

	return MovementResult{Movement: domain.InventoryMovement{
		ID:                fmt.Sprintf("mv-%06d", seq),
		IdempotencyKey:    req.IdempotencyKey,
		IngredientID:      req.IngredientID,
		Operation:         req.Operation,
		QuantityBase:      req.QuantityBase,
		Reason:            req.Reason,
		PreviousQuantity:  previous,
		ResultingQuantity: resulting,
		PerformedBy:       performerOrDefault(req.PerformedBy),
		CreatedAt:         createdAt,
	}}, nil
}

func performerOrDefault(performedBy string) string {
	if strings.TrimSpace(performedBy) == "" {
		return "unspecified"
	}
	return performedBy
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
