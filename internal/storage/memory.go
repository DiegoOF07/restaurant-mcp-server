package storage

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
)

// InMemoryRepository es la implementación de Repository que guarda todo en mapas.
// No persiste nada: existe para que las pruebas corran rápido y sin tocar el disco.
// El servidor real usa SQLiteRepository.
type InMemoryRepository struct {
	mu sync.Mutex

	dishes      map[string]domain.Dish
	ingredients map[string]domain.Ingredient
	recipes     map[string][]domain.RecipeItem // dishID -> items
	inventory   map[string]int64               // ingredientID -> cantidad base

	movementsByKey map[string]domain.InventoryMovement // idempotencyKey -> movimiento
	movementSeq    uint64
}

// NewInMemoryRepository crea un repositorio vacío. Usa Seed para cargar datos de ejemplo
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		dishes:         make(map[string]domain.Dish),
		ingredients:    make(map[string]domain.Ingredient),
		recipes:        make(map[string][]domain.RecipeItem),
		inventory:      make(map[string]int64),
		movementsByKey: make(map[string]domain.InventoryMovement),
	}
}

// AddDish registra o reemplaza un platillo. No forma parte de Repository y es una operación de
// carga de datos, no de consulta, y sólo la usan el seed y las pruebas.
func (r *InMemoryRepository) AddDish(d domain.Dish) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dishes[d.ID] = d
}

// FindDish busca un platillo por su identificador exacto.
func (r *InMemoryRepository) FindDish(id string) (domain.Dish, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.dishes[id]
	return d, ok
}

// SearchDishes devuelve los platillos cuyo nombre contenga query, sin distinguir
// mayúsculas. Un query vacío devuelve todos.
func (r *InMemoryRepository) SearchDishes(query string) []domain.Dish {
	r.mu.Lock()
	defer r.mu.Unlock()

	q := strings.ToLower(strings.TrimSpace(query))
	var results []domain.Dish
	for _, d := range r.dishes {
		if q == "" || strings.Contains(strings.ToLower(d.Name), q) {
			results = append(results, d)
		}
	}
	return results
}

// RecipeForDish devuelve los ingredientes de un platillo. El booleano distingue
// "el platillo no existe" de "existe pero no tiene receta cargada".
func (r *InMemoryRepository) RecipeForDish(dishID string) ([]domain.RecipeItem, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.dishes[dishID]; !ok {
		return nil, false
	}
	return r.recipes[dishID], true
}

// Ingredient busca un ingrediente por su identificador exacto.
func (r *InMemoryRepository) Ingredient(id string) (domain.Ingredient, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ing, ok := r.ingredients[id]
	return ing, ok
}

// SearchIngredients busca ingredientes cuyo nombre o identificador contenga query.
// Con query vacío devuelve todos
func (r *InMemoryRepository) SearchIngredients(query string) []domain.Ingredient {
	r.mu.Lock()
	defer r.mu.Unlock()

	q := strings.ToLower(strings.TrimSpace(query))
	results := make([]domain.Ingredient, 0, len(r.ingredients))
	for _, ing := range r.ingredients {
		if q == "" || strings.Contains(strings.ToLower(ing.Name), q) || strings.Contains(strings.ToLower(ing.ID), q) {
			results = append(results, ing)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}

// InventoryQuantity devuelve la existencia actual en la unidad base del ingrediente.
// El booleano indica si el ingrediente existe, no si tiene existencia: un ingrediente
// conocido sin movimientos devuelve (0, true).
func (r *InMemoryRepository) InventoryQuantity(ingredientID string) (int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ingredients[ingredientID]; !ok {
		return 0, false
	}
	return r.inventory[ingredientID], true // 0 por defecto si nunca se sembró
}

// MovementByKey devuelve el movimiento registrado bajo una clave de idempotencia.
// Sirve para auditar quién hizo cada ajuste (sección 13.2 del plan) y para verificar
// que un reintento no volvió a descontar.
func (r *InMemoryRepository) MovementByKey(key string) (domain.InventoryMovement, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.movementsByKey[key]
	return m, ok
}

// ApplyMovement aplica un ajuste de inventario de forma atómica donde toda la
// operación ocurre bajo un mismo lock, así que no puede quedar el
// inventario a medio actualizar ni perderse una actualización concurrente.
func (r *InMemoryRepository) ApplyMovement(req MovementRequest) (MovementResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.movementsByKey[req.IdempotencyKey]; ok {
		return MovementResult{Movement: existing, Idempotent: true}, nil
	}

	if _, ok := r.ingredients[req.IngredientID]; !ok {
		return MovementResult{}, domain.ErrIngredientNotFound
	}

	previous := r.inventory[req.IngredientID]
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

	if resulting < 0 {
		return MovementResult{}, domain.ErrInsufficientInventory
	}

	seq := atomic.AddUint64(&r.movementSeq, 1)
	movement := domain.InventoryMovement{
		ID:                fmt.Sprintf("mv-%06d", seq),
		IdempotencyKey:    req.IdempotencyKey,
		IngredientID:      req.IngredientID,
		Operation:         req.Operation,
		QuantityBase:      req.QuantityBase,
		Reason:            req.Reason,
		PreviousQuantity:  previous,
		ResultingQuantity: resulting,
		PerformedBy:       req.PerformedBy,
		CreatedAt:         time.Now().UTC(),
	}

	r.inventory[req.IngredientID] = resulting
	r.movementsByKey[req.IdempotencyKey] = movement

	return MovementResult{Movement: movement, Idempotent: false}, nil
}
