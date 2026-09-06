package storage

import "github.com/DiegoOF07/restaurant-mcp-server/internal/domain"

// Seed carga datos de ejemplo suficientes para los escenarios de demostración.
// Uso pensado para pruebas
func (r *InMemoryRepository) Seed() {
	r.mu.Lock()
	defer r.mu.Unlock()

	ingredients := []domain.Ingredient{
		{ID: "cheese", Name: "Queso cheddar", BaseUnit: domain.UnitGram, AllergenCategory: "lácteos"},
		{ID: "bun", Name: "Pan de hamburguesa", BaseUnit: domain.UnitPiece, AllergenCategory: "gluten"},
		{ID: "patty", Name: "Carne de res", BaseUnit: domain.UnitPiece, AllergenCategory: ""},
		{ID: "lettuce", Name: "Lechuga", BaseUnit: domain.UnitGram, AllergenCategory: ""},
		{ID: "flour", Name: "Harina de trigo", BaseUnit: domain.UnitGram, AllergenCategory: "gluten"},
		{ID: "chocolate", Name: "Chocolate amargo", BaseUnit: domain.UnitGram, AllergenCategory: "lácteos"},
		{ID: "milk", Name: "Leche entera", BaseUnit: domain.UnitMilliliter, AllergenCategory: "lácteos"},
		{ID: "walnuts", Name: "Nueces", BaseUnit: domain.UnitGram, AllergenCategory: "frutos secos"},
	}
	for _, ing := range ingredients {
		r.ingredients[ing.ID] = ing
	}

	dishes := []domain.Dish{
		{ID: "special-burger", Name: "Hamburguesa Especial", Description: "Hamburguesa de la casa con cheddar y lechuga", Active: true},
		{ID: "chocolate-cake", Name: "Pastel de Chocolate", Description: "Pastel de chocolate amargo con nueces", Active: true},
	}
	for _, d := range dishes {
		r.dishes[d.ID] = d
	}

	r.recipes["special-burger"] = []domain.RecipeItem{
		{DishID: "special-burger", IngredientID: "bun", QuantityPerServing: 1},
		{DishID: "special-burger", IngredientID: "patty", QuantityPerServing: 1},
		{DishID: "special-burger", IngredientID: "cheese", QuantityPerServing: 80},
		{DishID: "special-burger", IngredientID: "lettuce", QuantityPerServing: 20},
	}
	r.recipes["chocolate-cake"] = []domain.RecipeItem{
		{DishID: "chocolate-cake", IngredientID: "flour", QuantityPerServing: 150},
		{DishID: "chocolate-cake", IngredientID: "chocolate", QuantityPerServing: 100},
		{DishID: "chocolate-cake", IngredientID: "milk", QuantityPerServing: 50},
		{DishID: "chocolate-cake", IngredientID: "walnuts", QuantityPerServing: 20},
	}

	// Queso deliberadamente bajo para reproducir el ejemplo de alcanza para 1 porción pero no para 2
	r.inventory["cheese"] = 40
	r.inventory["bun"] = 10
	r.inventory["patty"] = 10
	r.inventory["lettuce"] = 500
	r.inventory["flour"] = 2000
	r.inventory["chocolate"] = 1000
	r.inventory["milk"] = 2000
	r.inventory["walnuts"] = 200
}
