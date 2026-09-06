package tools

// Los schemas se escriben como map[string]any para serializarse
// directamente a JSON Schema en tools/list

var searchDishesInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name": map[string]any{
			"type":        "string",
			"description": "Texto a buscar en el nombre del platillo. Cadena vacía devuelve todos los platillos.",
		},
	},
	"required":             []string{"name"},
	"additionalProperties": false,
}

var getDishAvailabilityInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"dishId": map[string]any{
			"type":        "string",
			"description": "Identificador del platillo.",
		},
		"servings": map[string]any{
			"type":        "integer",
			"minimum":     1,
			"description": "Número de porciones solicitadas.",
		},
	},
	"required":             []string{"dishId", "servings"},
	"additionalProperties": false,
}

var getRecipeDetailsInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"dishId": map[string]any{
			"type":        "string",
			"description": "Identificador del platillo.",
		},
	},
	"required":             []string{"dishId"},
	"additionalProperties": false,
}

var adjustInventoryInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"ingredientId": map[string]any{
			"type":        "string",
			"description": "Identificador del ingrediente.",
		},
		"operation": map[string]any{
			"type":        "string",
			"enum":        []string{"add", "subtract", "set"},
			"description": "Tipo de ajuste a aplicar sobre el inventario.",
		},
		"quantity": map[string]any{
			"type":             "number",
			"exclusiveMinimum": 0,
			"description":      "Cantidad a ajustar, en la unidad indicada en 'unit'.",
		},
		"unit": map[string]any{
			"type":        "string",
			"enum":        []string{"g", "kg", "ml", "l", "unit"},
			"description": "Unidad en la que se expresa 'quantity'.",
		},
		"reason": map[string]any{
			"type":        "string",
			"description": "Motivo del ajuste (por ejemplo: damaged, expired, correction).",
		},
		"idempotencyKey": map[string]any{
			"type":        "string",
			"description": "Identificador único de esta operación. Repetir la misma clave no vuelve a descontar.",
		},
	},
	"required":             []string{"ingredientId", "operation", "quantity", "unit", "idempotencyKey"},
	"additionalProperties": false,
}

var searchIngredientsInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name": map[string]any{
			"type":        "string",
			"description": "Texto a buscar en el nombre o el identificador del ingrediente. Cadena vacía devuelve todos los ingredientes.",
		},
	},
	"required":             []string{"name"},
	"additionalProperties": false,
}
