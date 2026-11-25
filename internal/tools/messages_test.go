package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
)

func text(r CallToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

func noInternals(t *testing.T, msg string) {
	t.Helper()
	for _, bad := range []string{`"cheese"`, "cheese", "subtract", "waiter", "idempotencyKey", "special-burger"} {
		if strings.Contains(msg, bad) {
			t.Errorf("el mensaje no debe mostrar %q: %s", bad, msg)
		}
	}
}

func TestAvailabilityMissingIncludesName(t *testing.T) {
	r := newTestRegistry(t)
	res, _ := r.Call("get_dish_availability", json.RawMessage(`{"dishId":"special-burger","servings":2}`))
	var parsed dishAvailabilityResult
	body, _ := json.Marshal(res.StructuredContent)
	json.Unmarshal(body, &parsed)
	if len(parsed.MissingIngredients) == 0 || parsed.MissingIngredients[0].Name != "Queso cheddar" {
		t.Fatalf("faltante sin nombre: %+v", parsed.MissingIngredients)
	}
	if got := text(res); got != "Hamburguesa Especial: alcanza para 0 porción(es) de 2." {
		t.Errorf("resumen = %q", got)
	}
}

func TestAdjustResultIncludesName(t *testing.T) {
	r := newTestRegistry(t)
	res, _ := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"lettuce","operation":"subtract","quantity":200,"unit":"g","reason":"se dañó","idempotencyKey":"n1"}`))
	out, ok := res.StructuredContent.(adjustInventoryResult)
	if !ok || out.Name != "Lechuga" {
		t.Fatalf("resultado sin nombre: %+v", res.StructuredContent)
	}
	if got := text(res); got != "Lechuga: 500 g → 300 g." {
		t.Errorf("resumen = %q", got)
	}
	noInternals(t, text(res))
}

func TestIdempotentMessageHasNoKey(t *testing.T) {
	r := newTestRegistry(t)
	args := json.RawMessage(`{"ingredientId":"flour","operation":"subtract","quantity":1,"unit":"kg","idempotencyKey":"same"}`)
	r.Call("adjust_inventory", args)
	res, _ := r.Call("adjust_inventory", args)
	msg := text(res)
	if !strings.Contains(msg, "Este ajuste ya se había registrado; no se volvió a aplicar.") {
		t.Errorf("mensaje idempotente = %q", msg)
	}
	if !strings.Contains(msg, "Harina de trigo: 2 kg → 1 kg.") {
		t.Errorf("debe usar kg legibles: %q", msg)
	}
	noInternals(t, msg)
}

func TestInsufficientMessageIsReadable(t *testing.T) {
	r := newTestRegistry(t)
	res, _ := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"cheese","operation":"subtract","quantity":200,"unit":"g","idempotencyKey":"k2"}`))
	msg := text(res)
	if msg != "No hay suficiente Queso cheddar: hay 40 g y se pedía restar 200 g." {
		t.Errorf("mensaje = %q", msg)
	}
}

func TestIncompatibleUnitMessageIsReadable(t *testing.T) {
	r := newTestRegistry(t)
	res, _ := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"cheese","operation":"add","quantity":1,"unit":"ml","idempotencyKey":"k3"}`))
	if msg := text(res); msg != "No puedo usar ml para Queso cheddar; se mide en gramos." {
		t.Errorf("mensaje = %q", msg)
	}
}

func TestUnknownItemsMessagesAreReadable(t *testing.T) {
	r := newTestRegistry(t)
	res, _ := r.Call("get_recipe_details", json.RawMessage(`{"dishId":"nope"}`))
	if msg := text(res); msg != "No encontré ese platillo en el menú." {
		t.Errorf("platillo = %q", msg)
	}
	res, _ = r.Call("adjust_inventory", json.RawMessage(`{"ingredientId":"tomate","operation":"add","quantity":1,"unit":"g","idempotencyKey":"k4"}`))
	if msg := text(res); msg != "No encontré ese ingrediente en el inventario." {
		t.Errorf("ingrediente = %q", msg)
	}
}

func TestRoleDeniedMessageIsReadable(t *testing.T) {
	r := newTestRegistryAs(t, domain.RoleWaiter)
	res, _ := r.Call("adjust_inventory", json.RawMessage(`{"ingredientId":"cheese","operation":"subtract","quantity":10,"unit":"g","idempotencyKey":"k5"}`))
	if msg := text(res); msg != "Tu rol (Mesero) no puede ajustar el inventario." {
		t.Errorf("mensaje = %q", msg)
	}
}

func TestReadableQuantity(t *testing.T) {
	cases := map[string]string{
		readableQuantity(80, domain.UnitGram):         "80 g",
		readableQuantity(1200, domain.UnitGram):       "1.2 kg",
		readableQuantity(2000, domain.UnitMilliliter): "2 l",
		readableQuantity(1, domain.UnitPiece):         "1 unidad",
		readableQuantity(3, domain.UnitPiece):         "3 unidades",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestIncompatibleUnitNamesItInSpanish(t *testing.T) {
	r := newTestRegistry(t)
	res, _ := r.Call("adjust_inventory", json.RawMessage(
		`{"ingredientId":"lettuce","operation":"add","quantity":1,"unit":"unit","idempotencyKey":"k6"}`))
	if msg := text(res); msg != "No puedo usar unidades para Lechuga; se mide en gramos." {
		t.Errorf("mensaje = %q", msg)
	}
}
