package auth_test

import (
	"strings"
	"testing"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/auth"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
)

func TestParseTokens(t *testing.T) {
	store, err := auth.ParseTokens("tok-a:admin:diego, tok-b:waiter:ana ")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	if store.Count() != 2 {
		t.Fatalf("Count = %d, se esperaban 2", store.Count())
	}

	admin, ok := store.Lookup("tok-a")
	if !ok || admin.Role != domain.RoleAdmin || admin.UserID != "diego" {
		t.Errorf("tok-a resolvió a %+v (ok=%t)", admin, ok)
	}

	waiter, ok := store.Lookup("tok-b")
	if !ok || waiter.Role != domain.RoleWaiter {
		t.Errorf("tok-b resolvió a %+v (ok=%t)", waiter, ok)
	}
}

func TestParseTokensEmpty(t *testing.T) {
	store, err := auth.ParseTokens("")
	if err != nil {
		t.Fatalf("una cadena vacía no es un error: %v", err)
	}
	if !store.Empty() {
		t.Error("un almacén sin credenciales debe reportarse vacío")
	}
}

// Una credencial mal escrita es un error de arranque, no una advertencia: un servidor remoto
// que arranca con la autenticación rota es peor que uno que no arranca.
func TestParseTokensRejectsMalformedEntries(t *testing.T) {
	cases := map[string]string{
		"faltan campos":                      "solo-el-token",
		"sobran campos":                      "tok:admin:diego:extra",
		"rol desconocido":                    "tok:gerente-supremo:diego",
		"token vacío":                        ":admin:diego",
		"usuario vacío":                      "tok:admin:",
		"una entrada inválida entre válidas": "tok-a:admin:diego,rota",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := auth.ParseTokens(raw); err == nil {
				t.Fatalf("ParseTokens(%q) debería haber fallado", raw)
			}
		})
	}
}

func TestLookupRejectsUnknownToken(t *testing.T) {
	store, err := auth.ParseTokens("tok-a:admin:diego")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}

	for _, candidate := range []string{"", "tok-b", "tok-", "tok-a "} {
		if _, ok := store.Lookup(candidate); ok {
			t.Errorf("Lookup(%q) aceptó un token que no existe", candidate)
		}
	}
}

// Un prefijo válido no debe pasar: si la comparación fuera por prefijo, adivinar un token
// se volvería trivial carácter a carácter.
func TestLookupIsNotPrefixMatch(t *testing.T) {
	store, err := auth.ParseTokens("tokenlargoysecreto:admin:diego")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}

	for i := 1; i < len("tokenlargoysecreto"); i++ {
		prefix := "tokenlargoysecreto"[:i]
		if _, ok := store.Lookup(prefix); ok {
			t.Fatalf("Lookup aceptó el prefijo %q", prefix)
		}
	}
}

// Un TokenStore nil se comporta como uno vacío: quien lo use no debe tener que comprobarlo
// antes de cada llamada.
func TestNilStoreIsSafe(t *testing.T) {
	var store *auth.TokenStore

	if !store.Empty() {
		t.Error("un almacén nil debe reportarse vacío")
	}
	if store.Count() != 0 {
		t.Error("un almacén nil no tiene credenciales")
	}
	if _, ok := store.Lookup("lo-que-sea"); ok {
		t.Error("un almacén nil no debe autenticar a nadie")
	}
}

func TestParseTokensErrorMentionsTheOffendingEntry(t *testing.T) {
	_, err := auth.ParseTokens("tok:rol-inventado:diego")
	if err == nil {
		t.Fatal("debería haber fallado")
	}
	if !strings.Contains(err.Error(), "rol-inventado") {
		t.Errorf("el error debería nombrar el rol inválido: %v", err)
	}
}
