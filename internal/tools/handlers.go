package tools

import (
	"encoding/json"
	"fmt"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
)

// RegisterRestaurantTools agrega las cuatro herramientas iniciales del
// restaurante a un Registry
func RegisterRestaurantTools(r *Registry) {
	r.register(Tool{
		Name:        "search_dishes",
		Description: "Search dishes by name (case-insensitive substring match). Use an empty name to list all dishes.",
		InputSchema: searchDishesInputSchema,
		handler:     handleSearchDishes,
	})
	r.register(Tool{
		Name:        "get_dish_availability",
		Description: "Calculate how many servings of a dish can currently be prepared and whether the requested number of servings is available.",
		InputSchema: getDishAvailabilityInputSchema,
		handler:     handleGetDishAvailability,
	})
	r.register(Tool{
		Name:        "get_recipe_details",
		Description: "Get the ingredients, quantities and allergen information for a dish.",
		InputSchema: getRecipeDetailsInputSchema,
		handler:     handleGetRecipeDetails,
	})
	r.register(Tool{
		Name:        "adjust_inventory",
		Description: "Register a loss, damage, or correction on an ingredient's inventory. Requires confirmation before use; safe to retry with the same idempotencyKey.",
		InputSchema: adjustInventoryInputSchema,
		handler:     handleAdjustInventory,
	})
}

// search_dishes

type searchDishesArgs struct {
	Name string `json:"name"`
}

type dishSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

func handleSearchDishes(repo storage.Repository, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args searchDishesArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("could not parse search_dishes arguments: " + err.Error())
	}

	dishes := repo.SearchDishes(args.Name)
	summaries := make([]dishSummary, 0, len(dishes))
	for _, d := range dishes {
		summaries = append(summaries, dishSummary{ID: d.ID, Name: d.Name, Description: d.Description, Active: d.Active})
	}

	return okResult(
		fmt.Sprintf("Found %d dish(es) matching %q.", len(summaries), args.Name),
		map[string]any{"dishes": summaries},
	), nil
}

// get_dish_availability

type getDishAvailabilityArgs struct {
	DishID   string `json:"dishId"`
	Servings int64  `json:"servings"`
}

type missingIngredient struct {
	IngredientID string `json:"ingredientId"`
	Required     int64  `json:"required"`
	Available    int64  `json:"available"`
	Unit         string `json:"unit"`
}

type dishAvailabilityResult struct {
	Available          bool                `json:"available"`
	RequestedServings  int64               `json:"requestedServings"`
	MaximumServings    int64               `json:"maximumServings"`
	MissingIngredients []missingIngredient `json:"missingIngredients"`
}

func handleGetDishAvailability(repo storage.Repository, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args getDishAvailabilityArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("could not parse get_dish_availability arguments: " + err.Error())
	}
	if args.Servings <= 0 {
		return CallToolResult{}, jsonrpc.InvalidParams("servings must be greater than zero")
	}

	dish, ok := repo.FindDish(args.DishID)
	if !ok {
		return errorResult(fmt.Sprintf("dish %q was not found", args.DishID)), nil
	}
	if !dish.Active {
		// Las recetas inactivas no se deben ofrecer como disponibles
		return okResult(
			fmt.Sprintf("%s is not currently active on the menu.", dish.Name),
			dishAvailabilityResult{RequestedServings: args.Servings, MaximumServings: 0, MissingIngredients: nil},
		), nil
	}

	recipe, _ := repo.RecipeForDish(args.DishID)

	var maxServings int64 = -1 // -1 = aún no calculado
	missing := make([]missingIngredient, 0)

	for _, item := range recipe {
		ing, ok := repo.Ingredient(item.IngredientID)
		if !ok {
			return CallToolResult{}, jsonrpc.InternalError("recipe references an unknown ingredient")
		}
		available, _ := repo.InventoryQuantity(item.IngredientID)

		possibleServings := available / item.QuantityPerServing
		if maxServings == -1 || possibleServings < maxServings {
			maxServings = possibleServings
		}

		required := item.QuantityPerServing * args.Servings
		if required > available {
			missing = append(missing, missingIngredient{
				IngredientID: ing.ID,
				Required:     required,
				Available:    available,
				Unit:         string(ing.BaseUnit),
			})
		}
	}
	if maxServings == -1 {
		maxServings = 0 // receta sin ingredientes registrados
	}

	result := dishAvailabilityResult{
		Available:          len(missing) == 0,
		RequestedServings:  args.Servings,
		MaximumServings:    maxServings,
		MissingIngredients: missing,
	}

	summary := fmt.Sprintf("%s: requested %d serving(s), maximum currently possible is %d.", dish.Name, args.Servings, maxServings)
	return okResult(summary, result), nil
}

// get_recipe_details

type getRecipeDetailsArgs struct {
	DishID string `json:"dishId"`
}

type recipeIngredient struct {
	IngredientID       string `json:"ingredientId"`
	Name               string `json:"name"`
	QuantityPerServing int64  `json:"quantityPerServing"`
	Unit               string `json:"unit"`
	AllergenCategory   string `json:"allergenCategory,omitempty"`
}

type recipeDetailsResult struct {
	DishID      string             `json:"dishId"`
	DishName    string             `json:"dishName"`
	Ingredients []recipeIngredient `json:"ingredients"`
}

func handleGetRecipeDetails(repo storage.Repository, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args getRecipeDetailsArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("could not parse get_recipe_details arguments: " + err.Error())
	}

	dish, ok := repo.FindDish(args.DishID)
	if !ok {
		return errorResult(fmt.Sprintf("dish %q was not found", args.DishID)), nil
	}

	recipe, _ := repo.RecipeForDish(args.DishID)
	ingredients := make([]recipeIngredient, 0, len(recipe))
	for _, item := range recipe {
		ing, ok := repo.Ingredient(item.IngredientID)
		if !ok {
			return CallToolResult{}, jsonrpc.InternalError("recipe references an unknown ingredient")
		}
		ingredients = append(ingredients, recipeIngredient{
			IngredientID:       ing.ID,
			Name:               ing.Name,
			QuantityPerServing: item.QuantityPerServing,
			Unit:               string(ing.BaseUnit),
			AllergenCategory:   ing.AllergenCategory,
		})
	}

	result := recipeDetailsResult{DishID: dish.ID, DishName: dish.Name, Ingredients: ingredients}
	return okResult(fmt.Sprintf("%s has %d ingredient(s) on record.", dish.Name, len(ingredients)), result), nil
}

// adjust_inventory

type adjustInventoryArgs struct {
	IngredientID   string  `json:"ingredientId"`
	Operation      string  `json:"operation"`
	Quantity       float64 `json:"quantity"`
	Unit           string  `json:"unit"`
	Reason         string  `json:"reason"`
	IdempotencyKey string  `json:"idempotencyKey"`
}

type adjustInventoryResult struct {
	MovementID        string `json:"movementId"`
	IngredientID      string `json:"ingredientId"`
	Operation         string `json:"operation"`
	PreviousQuantity  int64  `json:"previousQuantity"`
	ResultingQuantity int64  `json:"resultingQuantity"`
	Unit              string `json:"unit"`
	Idempotent        bool   `json:"idempotent"`
}

func handleAdjustInventory(repo storage.Repository, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args adjustInventoryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("could not parse adjust_inventory arguments: " + err.Error())
	}
	if args.IdempotencyKey == "" {
		return CallToolResult{}, jsonrpc.InvalidParams("idempotencyKey is required")
	}
	if args.Operation != "add" && args.Operation != "subtract" && args.Operation != "set" {
		return CallToolResult{}, jsonrpc.InvalidParams("operation must be one of: add, subtract, set")
	}

	ing, ok := repo.Ingredient(args.IngredientID)
	if !ok {
		return errorResult(fmt.Sprintf("ingredient %q was not found", args.IngredientID)), nil
	}

	quantityBase, err := domain.ToBaseUnits(args.Quantity, domain.Unit(args.Unit), ing.BaseUnit)
	if err != nil {
		return errorResult(fmt.Sprintf(
			"unit %q is not compatible with ingredient %q (base unit: %s)", args.Unit, ing.ID, ing.BaseUnit,
		)), nil
	}

	result, err := repo.ApplyMovement(storage.MovementRequest{
		IdempotencyKey: args.IdempotencyKey,
		IngredientID:   args.IngredientID,
		Operation:      args.Operation,
		QuantityBase:   quantityBase,
		Reason:         args.Reason,
		PerformedBy:    "unspecified",
	})
	if err != nil {
		switch err {
		case domain.ErrInsufficientInventory:
			return errorResult(fmt.Sprintf(
				"cannot subtract %d %s of %q: only %d available", quantityBase, ing.BaseUnit, ing.ID,
				mustCurrentQuantity(repo, ing.ID),
			)), nil
		case domain.ErrIngredientNotFound:
			return errorResult(fmt.Sprintf("ingredient %q was not found", args.IngredientID)), nil
		default:
			return CallToolResult{}, jsonrpc.InternalError("could not apply inventory movement")
		}
	}

	out := adjustInventoryResult{
		MovementID:        result.Movement.ID,
		IngredientID:      result.Movement.IngredientID,
		Operation:         result.Movement.Operation,
		PreviousQuantity:  result.Movement.PreviousQuantity,
		ResultingQuantity: result.Movement.ResultingQuantity,
		Unit:              string(ing.BaseUnit),
		Idempotent:        result.Idempotent,
	}

	summary := fmt.Sprintf(
		"%s: %s went from %d to %d %s.", ing.Name, args.Operation, out.PreviousQuantity, out.ResultingQuantity, ing.BaseUnit,
	)
	if result.Idempotent {
		summary += " (idempotencyKey already applied; no change was made)"
	}
	return okResult(summary, out), nil
}

func mustCurrentQuantity(repo storage.Repository, ingredientID string) int64 {
	qty, _ := repo.InventoryQuantity(ingredientID)
	return qty
}
