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
		Description: "Busca platillos del menú por nombre (coincidencia parcial, sin distinguir mayúsculas). Usa un nombre vacío para listar todos los platillos.",
		InputSchema: searchDishesInputSchema,
		handler:     handleSearchDishes,
	})
	r.register(Tool{
		Name:        "search_ingredients",
		Description: "Busca ingredientes por nombre o identificador (coincidencia parcial, sin distinguir mayúsculas) y devuelve su identificador, unidad base, alérgeno y existencia actual. Úsala para traducir el nombre que dice el usuario al identificador que exigen las demás herramientas. Un nombre vacío devuelve todos los ingredientes.",
		InputSchema: searchIngredientsInputSchema,
		handler:     handleSearchIngredients,
	})
	r.register(Tool{
		Name:        "get_dish_availability",
		Description: "Calcula cuántas porciones de un platillo se pueden preparar ahora mismo con el inventario actual, y si alcanza para las porciones solicitadas. Úsala siempre en vez de calcular tú mismo.",
		InputSchema: getDishAvailabilityInputSchema,
		handler:     handleGetDishAvailability,
	})
	r.register(Tool{
		Name:        "get_recipe_details",
		Description: "Devuelve los ingredientes, las cantidades por porción y los alérgenos registrados de un platillo. Única fuente válida de información de alérgenos.",
		InputSchema: getRecipeDetailsInputSchema,
		handler:     handleGetRecipeDetails,
	})
	r.register(Tool{
		Name: "adjust_inventory",
		// Sección 13.1 del plan: los ajustes de inventario se restringen a cocina y administración.
		RequiredRoles: domain.RoleSet{domain.RoleCook, domain.RoleAdmin},
		Description:   "Registra una pérdida, daño o corrección en el inventario de un ingrediente. Requiere confirmación del usuario antes de ejecutarse; repetir la llamada con el mismo idempotencyKey no vuelve a descontar.",
		InputSchema:   adjustInventoryInputSchema,
		handler:       handleAdjustInventory,
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

func handleSearchDishes(ctx CallContext, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args searchDishesArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("no se pudieron interpretar los argumentos de search_dishes: " + err.Error())
	}

	dishes := ctx.Repo.SearchDishes(args.Name)
	summaries := make([]dishSummary, 0, len(dishes))
	for _, d := range dishes {
		summaries = append(summaries, dishSummary{ID: d.ID, Name: d.Name, Description: d.Description, Active: d.Active})
	}

	return okResult(
		fmt.Sprintf("Se encontraron %d platillo(s) que coinciden con %q.", len(summaries), args.Name),
		map[string]any{"dishes": summaries},
	), nil
}

// search_ingredients

type searchIngredientsArgs struct {
	Name string `json:"name"`
}

type ingredientSummary struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	BaseUnit          string `json:"baseUnit"`
	AllergenCategory  string `json:"allergenCategory,omitempty"`
	AvailableQuantity int64  `json:"availableQuantity"`
}

func handleSearchIngredients(ctx CallContext, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args searchIngredientsArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("no se pudieron interpretar los argumentos de search_ingredients: " + err.Error())
	}

	ingredients := ctx.Repo.SearchIngredients(args.Name)
	summaries := make([]ingredientSummary, 0, len(ingredients))
	for _, ing := range ingredients {
		available, _ := ctx.Repo.InventoryQuantity(ing.ID)
		summaries = append(summaries, ingredientSummary{
			ID:                ing.ID,
			Name:              ing.Name,
			BaseUnit:          string(ing.BaseUnit),
			AllergenCategory:  ing.AllergenCategory,
			AvailableQuantity: available,
		})
	}

	return okResult(
		fmt.Sprintf("Se encontraron %d ingrediente(s) que coinciden con %q.", len(summaries), args.Name),
		map[string]any{"ingredients": summaries},
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

func handleGetDishAvailability(ctx CallContext, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args getDishAvailabilityArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("no se pudieron interpretar los argumentos de get_dish_availability: " + err.Error())
	}
	if args.Servings <= 0 {
		return CallToolResult{}, jsonrpc.InvalidParams("servings debe ser mayor que cero")
	}

	dish, ok := ctx.Repo.FindDish(args.DishID)
	if !ok {
		return errorResult(fmt.Sprintf("no existe el platillo %q", args.DishID)), nil
	}
	if !dish.Active {
		// Las recetas inactivas no se deben ofrecer como disponibles
		return okResult(
			fmt.Sprintf("%s no está activo en el menú en este momento.", dish.Name),
			dishAvailabilityResult{RequestedServings: args.Servings, MaximumServings: 0, MissingIngredients: nil},
		), nil
	}

	recipe, _ := ctx.Repo.RecipeForDish(args.DishID)

	var maxServings int64 = -1 // -1 = aún no calculado
	missing := make([]missingIngredient, 0)

	for _, item := range recipe {
		ing, ok := ctx.Repo.Ingredient(item.IngredientID)
		if !ok {
			return CallToolResult{}, jsonrpc.InternalError("la receta referencia un ingrediente inexistente")
		}
		available, _ := ctx.Repo.InventoryQuantity(item.IngredientID)

		// Una cantidad por porción no positiva es un dato corrupto, no una consulta inválida:
		// dividir entre cero haría panic y mataría el proceso completo del servidor.
		if item.QuantityPerServing <= 0 {
			return CallToolResult{}, jsonrpc.InternalError("la receta tiene una cantidad por porción inválida")
		}

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
		// No basta con que no falte ningún ingrediente: un platillo sin receta registrada
		// tampoco tiene faltantes, y aun así no se puede preparar.
		Available:          len(missing) == 0 && maxServings >= args.Servings,
		RequestedServings:  args.Servings,
		MaximumServings:    maxServings,
		MissingIngredients: missing,
	}

	summary := fmt.Sprintf("%s: se solicitaron %d porción(es); el máximo posible ahora mismo es %d.", dish.Name, args.Servings, maxServings)
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

func handleGetRecipeDetails(ctx CallContext, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args getRecipeDetailsArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("no se pudieron interpretar los argumentos de get_recipe_details: " + err.Error())
	}

	dish, ok := ctx.Repo.FindDish(args.DishID)
	if !ok {
		return errorResult(fmt.Sprintf("no existe el platillo %q", args.DishID)), nil
	}

	recipe, _ := ctx.Repo.RecipeForDish(args.DishID)
	ingredients := make([]recipeIngredient, 0, len(recipe))
	for _, item := range recipe {
		ing, ok := ctx.Repo.Ingredient(item.IngredientID)
		if !ok {
			return CallToolResult{}, jsonrpc.InternalError("la receta referencia un ingrediente inexistente")
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
	return okResult(fmt.Sprintf("%s tiene %d ingrediente(s) registrados.", dish.Name, len(ingredients)), result), nil
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

func handleAdjustInventory(ctx CallContext, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	var args adjustInventoryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return CallToolResult{}, jsonrpc.InvalidParams("no se pudieron interpretar los argumentos de adjust_inventory: " + err.Error())
	}
	if args.IdempotencyKey == "" {
		return CallToolResult{}, jsonrpc.InvalidParams("idempotencyKey es obligatorio")
	}
	if args.Operation != "add" && args.Operation != "subtract" && args.Operation != "set" {
		return CallToolResult{}, jsonrpc.InvalidParams("operation debe ser una de: add, subtract, set")
	}

	ing, ok := ctx.Repo.Ingredient(args.IngredientID)
	if !ok {
		return errorResult(fmt.Sprintf("no existe el ingrediente %q", args.IngredientID)), nil
	}

	quantityBase, err := domain.ToBaseUnits(args.Quantity, domain.Unit(args.Unit), ing.BaseUnit)
	if err != nil {
		return errorResult(fmt.Sprintf(
			"la unidad %q no es compatible con el ingrediente %q (unidad base: %s)", args.Unit, ing.ID, ing.BaseUnit,
		)), nil
	}

	result, err := ctx.Repo.ApplyMovement(storage.MovementRequest{
		IdempotencyKey: args.IdempotencyKey,
		IngredientID:   args.IngredientID,
		Operation:      args.Operation,
		QuantityBase:   quantityBase,
		Reason:         args.Reason,
		PerformedBy:    ctx.UserID,
	})
	if err != nil {
		switch err {
		case domain.ErrInsufficientInventory:
			return errorResult(fmt.Sprintf(
				"la operación %q de %d %s sobre %q dejaría el inventario en negativo; disponible actualmente: %d %s",
				args.Operation, quantityBase, ing.BaseUnit, ing.ID,
				mustCurrentQuantity(ctx.Repo, ing.ID), ing.BaseUnit,
			)), nil
		case domain.ErrIngredientNotFound:
			return errorResult(fmt.Sprintf("no existe el ingrediente %q", args.IngredientID)), nil
		default:
			return CallToolResult{}, jsonrpc.InternalError("no se pudo aplicar el movimiento de inventario")
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
		"%s: la operación %s llevó el inventario de %d a %d %s.", ing.Name, args.Operation, out.PreviousQuantity, out.ResultingQuantity, ing.BaseUnit,
	)
	if result.Idempotent {
		summary += " (ese idempotencyKey ya se había aplicado; no se hizo ningún cambio)"
	}
	return okResult(summary, out), nil
}

func mustCurrentQuantity(repo storage.Repository, ingredientID string) int64 {
	qty, _ := repo.InventoryQuantity(ingredientID)
	return qty
}
