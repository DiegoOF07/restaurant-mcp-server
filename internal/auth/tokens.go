// Package auth resuelve QUIÉN está del otro lado de una conexión remota.
package auth

import (
	"crypto/subtle"
	"fmt"
	"strings"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
)

// Principal es la identidad que un token acredita.
type Principal struct {
	Role   domain.Role
	UserID string
}

// TokenStore mapea tokens a identidades.
//
// Por qué el rol viene del token y NO de una cabecera: con stdio el servidor es un
// subproceso lanzado por el propio usuario, así que confiar en el entorno es razonable —
// quien puede fijar la variable ya podía ejecutar el binario. Por HTTP eso deja de valer:
// cualquiera que alcance el puerto podría mandar "X-Role: admin" y ascenderse solo. Ligar
// el rol a un secreto que el servidor conoce de antemano es lo único que sobrevive a un
// cliente hostil.
type TokenStore struct {
	entries []entry
}

type entry struct {
	token     string
	principal Principal
}

// ParseTokens interpreta la configuración de credenciales con el formato
// "token:rol:usuario,token:rol:usuario". Un formato inválido es un error de arranque y no
// una advertencia: un servidor remoto que arranca con la autenticación mal escrita es peor
// que uno que no arranca.
func ParseTokens(raw string) (*TokenStore, error) {
	store := &TokenStore{}

	for _, chunk := range strings.Split(raw, ",") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}

		parts := strings.Split(chunk, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("credencial inválida %q: se esperaba el formato token:rol:usuario", chunk)
		}

		token := strings.TrimSpace(parts[0])
		if token == "" {
			return nil, fmt.Errorf("credencial inválida %q: el token no puede ir vacío", chunk)
		}

		role, ok := domain.ParseRole(parts[1])
		if !ok {
			return nil, fmt.Errorf("credencial inválida %q: %q no es un rol conocido", chunk, parts[1])
		}

		userID := strings.TrimSpace(parts[2])
		if userID == "" {
			return nil, fmt.Errorf("credencial inválida %q: el usuario no puede ir vacío", chunk)
		}

		store.entries = append(store.entries, entry{
			token:     token,
			principal: Principal{Role: role, UserID: userID},
		})
	}

	return store, nil
}

// Empty indica si no hay ninguna credencial configurada.
func (s *TokenStore) Empty() bool { return s == nil || len(s.entries) == 0 }

// Count devuelve cuántas credenciales hay, para poder registrarlo sin exponer los tokens.
func (s *TokenStore) Count() int {
	if s == nil {
		return 0
	}
	return len(s.entries)
}

// Lookup resuelve la identidad de un token.
//
// El recorrido es lineal y la comparación de tiempo constante a propósito: comparar con ==
// permitiría deducir un token válido midiendo cuánto tarda el rechazo. Con un puñado de
// credenciales el costo de recorrerlas todas siempre es irrelevante.
func (s *TokenStore) Lookup(candidate string) (Principal, bool) {
	if s == nil {
		return Principal{}, false
	}

	var found Principal
	matched := false
	for _, e := range s.entries {
		if subtle.ConstantTimeCompare([]byte(e.token), []byte(candidate)) == 1 {
			found = e.principal
			matched = true
		}
	}
	return found, matched
}
