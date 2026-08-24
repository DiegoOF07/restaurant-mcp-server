// Package storage define el acceso a datos del restaurante detrás de una interfaz
package storage

import "github.com/DiegoOF07/restaurant-mcp-server/internal/domain"

// MovementRequest son los datos necesarios para aplicar un ajuste de inventario
type MovementRequest struct {
	IdempotencyKey string
	IngredientID   string
	Operation      string // "add" | "subtract" | "set"
	QuantityBase   int64
	Reason         string
	PerformedBy    string
}

// MovementResult es lo que se devuelve tras aplicar o reutilizar un
// movimiento. Idempotent=true indica que ya existía un movimiento con esa
// misma IdempotencyKey y no se descontó de nuevo
type MovementResult struct {
	Movement   domain.InventoryMovement
	Idempotent bool
}

// Repository es el contrato que necesita la capa de herramientas
type Repository interface {
	FindDish(id string) (domain.Dish, bool)
	SearchDishes(query string) []domain.Dish
	RecipeForDish(dishID string) ([]domain.RecipeItem, bool)
	Ingredient(id string) (domain.Ingredient, bool)
	InventoryQuantity(ingredientID string) (int64, bool)
	ApplyMovement(req MovementRequest) (MovementResult, error)
}
