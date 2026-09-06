package storage

import "github.com/DiegoOF07/restaurant-mcp-server/internal/domain"

// SeedData es el catálogo de demostración en una forma independiente del almacenamiento.
// Vive aparte para que la versión en memoria y la de SQLite se siembren desde exactamente
// los mismos datos
type SeedData struct {
	Ingredients []domain.Ingredient
	Dishes      []domain.Dish
	Recipes     map[string][]domain.RecipeItem
	Inventory   map[string]int64
}

// DemoSeed devuelve los datos de ejemplo suficientes para los escenarios de demostración.
func DemoSeed() SeedData {
	return SeedData{
		Ingredients: []domain.Ingredient{
			{ID: "cheese", Name: "Queso cheddar", BaseUnit: domain.UnitGram, AllergenCategory: "lácteos"},
			{ID: "bun", Name: "Pan de hamburguesa", BaseUnit: domain.UnitPiece, AllergenCategory: "gluten"},
			{ID: "patty", Name: "Carne de res", BaseUnit: domain.UnitPiece, AllergenCategory: ""},
			{ID: "lettuce", Name: "Lechuga", BaseUnit: domain.UnitGram, AllergenCategory: ""},
			{ID: "flour", Name: "Harina de trigo", BaseUnit: domain.UnitGram, AllergenCategory: "gluten"},
			{ID: "chocolate", Name: "Chocolate amargo", BaseUnit: domain.UnitGram, AllergenCategory: "lácteos"},
			{ID: "milk", Name: "Leche entera", BaseUnit: domain.UnitMilliliter, AllergenCategory: "lácteos"},
			{ID: "walnuts", Name: "Nueces", BaseUnit: domain.UnitGram, AllergenCategory: "frutos secos"},
		},
		Dishes: []domain.Dish{
			{ID: "special-burger", Name: "Hamburguesa Especial", Description: "Hamburguesa de la casa con cheddar y lechuga", Active: true},
			{ID: "chocolate-cake", Name: "Pastel de Chocolate", Description: "Pastel de chocolate amargo con nueces", Active: true},
		},
		Recipes: map[string][]domain.RecipeItem{
			"special-burger": {
				{DishID: "special-burger", IngredientID: "bun", QuantityPerServing: 1},
				{DishID: "special-burger", IngredientID: "patty", QuantityPerServing: 1},
				{DishID: "special-burger", IngredientID: "cheese", QuantityPerServing: 80},
				{DishID: "special-burger", IngredientID: "lettuce", QuantityPerServing: 20},
			},
			"chocolate-cake": {
				{DishID: "chocolate-cake", IngredientID: "flour", QuantityPerServing: 150},
				{DishID: "chocolate-cake", IngredientID: "chocolate", QuantityPerServing: 100},
				{DishID: "chocolate-cake", IngredientID: "milk", QuantityPerServing: 50},
				{DishID: "chocolate-cake", IngredientID: "walnuts", QuantityPerServing: 20},
			},
		},
		Inventory: map[string]int64{
			// Queso deliberadamente bajo para reproducir el ejemplo de "alcanza para 1 porción
			// pero no para 2".
			"cheese":    40,
			"bun":       10,
			"patty":     10,
			"lettuce":   500,
			"flour":     2000,
			"chocolate": 1000,
			"milk":      2000,
			"walnuts":   200,
		},
	}
}

// Seed carga el catálogo de demostración en el repositorio en memoria.
func (r *InMemoryRepository) Seed() {
	data := DemoSeed()

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ing := range data.Ingredients {
		r.ingredients[ing.ID] = ing
	}
	for _, d := range data.Dishes {
		r.dishes[d.ID] = d
	}
	for dishID, items := range data.Recipes {
		r.recipes[dishID] = items
	}
	for ingID, qty := range data.Inventory {
		r.inventory[ingID] = qty
	}
}
