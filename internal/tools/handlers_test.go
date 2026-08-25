package tools

import (
	"encoding/json"
	"testing"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	repo := storage.NewInMemoryRepository()
	repo.Seed()
	r := NewRegistry(repo)
	RegisterRestaurantTools(r)
	return r
}

func TestRegistry_List_ReturnsFourTools(t *testing.T) {
	r := newTestRegistry(t)
	list := r.List()
	if len(list) != 4 {
		t.Fatalf("esperaba 4 herramientas, obtuve %d", len(list))
	}
}

func TestCall_UnknownTool_IsProtocolError(t *testing.T) {
	r := newTestRegistry(t)
	_, errObj := r.Call("does_not_exist", json.RawMessage(`{}`))
	if errObj == nil {
		t.Fatal("esperaba un ErrorObject de protocolo")
	}
}

func TestSearchDishes_FindsByPartialName(t *testing.T) {
	r := newTestRegistry(t)
	result, errObj := r.Call("search_dishes", json.RawMessage(`{"name":"burger"}`))
	if errObj != nil {
		t.Fatalf("no esperaba error: %v", errObj)
	}

	body, _ := json.Marshal(result.StructuredContent)
	var parsed struct {
		Dishes []dishSummary `json:"dishes"`
	}
	json.Unmarshal(body, &parsed)

	if len(parsed.Dishes) != 1 || parsed.Dishes[0].ID != "special-burger" {
		t.Fatalf("resultado inesperado: %+v", parsed.Dishes)
	}
}

func TestGetDishAvailability_InsufficientCheese(t *testing.T) {
	// Replica el ejemplo de la sección 9.3 del plan: 2 porciones de
	// special-burger requieren 160g de queso, solo hay 40g -> alcanza para 0.
	r := newTestRegistry(t)
	result, errObj := r.Call("get_dish_availability", json.RawMessage(`{"dishId":"special-burger","servings":2}`))
	if errObj != nil {
		t.Fatalf("no esperaba error: %v", errObj)
	}

	body, _ := json.Marshal(result.StructuredContent)
	var parsed dishAvailabilityResult
	json.Unmarshal(body, &parsed)

	if parsed.Available {
		t.Error("esperaba available=false")
	}
	if parsed.MaximumServings != 0 {
		t.Errorf("esperaba maximumServings=0 (40g de queso alcanza para 0 porciones de 80g), obtuve %d", parsed.MaximumServings)
	}
	found := false
	for _, m := range parsed.MissingIngredients {
		if m.IngredientID == "cheese" && m.Required == 160 && m.Available == 40 {
			found = true
		}
	}
	if !found {
		t.Errorf("esperaba encontrar cheese como ingrediente faltante con required=160 available=40, obtuve %+v", parsed.MissingIngredients)
	}
}

func TestGetDishAvailability_UnknownDish_IsBusinessError(t *testing.T) {
	r := newTestRegistry(t)
	result, errObj := r.Call("get_dish_availability", json.RawMessage(`{"dishId":"does-not-exist","servings":1}`))
	if errObj != nil {
		t.Fatalf("un platillo inexistente es error de negocio, no de protocolo: %v", errObj)
	}
	if !result.IsError {
		t.Error("esperaba IsError=true")
	}
}

func TestGetRecipeDetails_IncludesAllergens(t *testing.T) {
	r := newTestRegistry(t)
	result, errObj := r.Call("get_recipe_details", json.RawMessage(`{"dishId":"chocolate-cake"}`))
	if errObj != nil {
		t.Fatalf("no esperaba error: %v", errObj)
	}

	body, _ := json.Marshal(result.StructuredContent)
	var parsed recipeDetailsResult
	json.Unmarshal(body, &parsed)

	foundNuts := false
	for _, ing := range parsed.Ingredients {
		if ing.IngredientID == "walnuts" && ing.AllergenCategory == "nuts" {
			foundNuts = true
		}
	}
	if !foundNuts {
		t.Errorf("esperaba encontrar walnuts con allergenCategory=nuts, obtuve %+v", parsed.Ingredients)
	}
}

func TestAdjustInventory_DamagedCheese_MatchesPlanExample(t *testing.T) {
	// Replica el ejemplo de descontar 2000g (=2kg) de queso dañado. 
	// El seed inicial tiene 40g
	r := newTestRegistry(t)

	_, errObj := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"cheese","operation":"add","quantity":5,"unit":"kg","reason":"restock","idempotencyKey":"restock-1"}`,
	))
	if errObj != nil {
		t.Fatalf("no esperaba error en restock: %v", errObj)
	}

	result, errObj := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"cheese","operation":"subtract","quantity":2000,"unit":"g","reason":"damaged","idempotencyKey":"damage-1"}`,
	))
	if errObj != nil {
		t.Fatalf("no esperaba error: %v", errObj)
	}

	body, _ := json.Marshal(result.StructuredContent)
	var parsed adjustInventoryResult
	json.Unmarshal(body, &parsed)

	// 40g (seed) + 5000g (restock) - 2000g (damaged) = 3040g
	if parsed.ResultingQuantity != 3040 {
		t.Errorf("esperaba resultingQuantity=3040, obtuve %d", parsed.ResultingQuantity)
	}
	if parsed.Idempotent {
		t.Error("primera ejecución no debería marcarse como idempotente")
	}
}

func TestAdjustInventory_RepeatedIdempotencyKey_DoesNotDoubleSubtract(t *testing.T) {
	// Las operaciones repetidas con la misma clave de idempotencia no deben descontar nuevamente
	r := newTestRegistry(t)
	args := json.RawMessage(
		`{"ingredientId":"lettuce","operation":"subtract","quantity":100,"unit":"g","reason":"damaged","idempotencyKey":"same-key"}`,
	)

	first, errObj := r.Call("adjust_inventory", args)
	if errObj != nil {
		t.Fatalf("no esperaba error: %v", errObj)
	}
	second, errObj := r.Call("adjust_inventory", args)
	if errObj != nil {
		t.Fatalf("no esperaba error: %v", errObj)
	}

	var firstResult, secondResult adjustInventoryResult
	b1, _ := json.Marshal(first.StructuredContent)
	b2, _ := json.Marshal(second.StructuredContent)
	json.Unmarshal(b1, &firstResult)
	json.Unmarshal(b2, &secondResult)

	if firstResult.ResultingQuantity != secondResult.ResultingQuantity {
		t.Errorf("la segunda llamada con la misma idempotencyKey no debería cambiar el resultado: %d vs %d",
			firstResult.ResultingQuantity, secondResult.ResultingQuantity)
	}
	if !secondResult.Idempotent {
		t.Error("la segunda llamada debería marcarse como idempotent=true")
	}
}

func TestAdjustInventory_InsufficientInventory_IsBusinessError(t *testing.T) {
	// No permitir resultados negativos
	r := newTestRegistry(t)
	result, errObj := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"cheese","operation":"subtract","quantity":9999,"unit":"g","reason":"damaged","idempotencyKey":"too-much"}`,
	))
	if errObj != nil {
		t.Fatalf("inventario insuficiente es error de negocio, no de protocolo: %v", errObj)
	}
	if !result.IsError {
		t.Error("esperaba IsError=true")
	}
}

func TestAdjustInventory_IncompatibleUnit_IsBusinessError(t *testing.T) {
	// cheese tiene base_unit=g; pedir "ml" debe rechazarse
	r := newTestRegistry(t)
	result, errObj := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"cheese","operation":"add","quantity":100,"unit":"ml","reason":"restock","idempotencyKey":"bad-unit"}`,
	))
	if errObj != nil {
		t.Fatalf("unidad incompatible es error de negocio, no de protocolo: %v", errObj)
	}
	if !result.IsError {
		t.Error("esperaba IsError=true")
	}
}

func TestAdjustInventory_MissingIdempotencyKey_IsProtocolError(t *testing.T) {
	r := newTestRegistry(t)
	_, errObj := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"cheese","operation":"add","quantity":1,"unit":"g","reason":"restock"}`,
	))
	if errObj == nil {
		t.Fatal("esperaba error de protocolo por idempotencyKey ausente")
	}
}