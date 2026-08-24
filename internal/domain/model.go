// Package domain contiene el modelo de negocio del restaurante con platillos,
// ingredientes, recetas e inventario
package domain

import "time"

// Unit es una unidad de medida soportada. Todo se almacena en la unidad base del ingrediente
type Unit string

const (
	UnitGram       Unit = "g"
	UnitKilogram   Unit = "kg"
	UnitMilliliter Unit = "ml"
	UnitLiter      Unit = "l"
	UnitPiece      Unit = "unit"
)

// Dish es un platillo del menú
type Dish struct {
	ID          string
	Name        string
	Description string
	Active      bool
}

// Ingredient es un ingrediente con su unidad base y categoría de alérgeno
// AllergenCategory queda vacío si el ingrediente no es un alérgeno registrado
type Ingredient struct {
	ID               string
	Name             string
	BaseUnit         Unit
	AllergenCategory string
}

// RecipeItem es la cantidad de un ingrediente requerida por porción de un
// platillo, expresada en la unidad base del ingrediente
type RecipeItem struct {
	DishID             string
	IngredientID       string
	QuantityPerServing int64 // en unidad base del ingrediente
}

// InventoryMovement es el registro auditable de un ajuste de inventario
type InventoryMovement struct {
	ID                string
	IdempotencyKey    string
	IngredientID      string
	Operation         string // "add" | "subtract" | "set"
	QuantityBase      int64  // en unidad base del ingrediente
	Reason            string
	PreviousQuantity  int64
	ResultingQuantity int64
	PerformedBy       string
	CreatedAt         time.Time
}
