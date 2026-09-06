package domain

import "strings"

// Role es el rol del usuario en cuyo nombre actúa esta conexión MCP.
// La sección 13.1 del plan exige distinguir al menos estos tres y validar los permisos
// EN EL SERVIDOR, no solamente en la interfaz: el host es código cliente y un cliente
// distinto (o modificado) podría no aplicar ninguna restricción.
type Role string

const (
	RoleWaiter Role = "waiter"
	RoleCook   Role = "cook"
	RoleAdmin  Role = "admin"
)

// ParseRole normaliza e interpreta el nombre de un rol.
// Devuelve false si no corresponde a ningún rol conocido; quien llama decide qué hacer,
// pero NUNCA debe caer a un rol con más permisos.
func ParseRole(raw string) (Role, bool) {
	switch Role(strings.ToLower(strings.TrimSpace(raw))) {
	case RoleWaiter:
		return RoleWaiter, true
	case RoleCook:
		return RoleCook, true
	case RoleAdmin:
		return RoleAdmin, true
	default:
		return "", false
	}
}

// RoleSet es un conjunto de roles autorizados para una operación.
type RoleSet []Role

// Allows indica si el rol dado pertenece al conjunto.
// Un conjunto vacío significa "sin restricción": toda operación de solo lectura lo usa.
func (rs RoleSet) Allows(role Role) bool {
	if len(rs) == 0 {
		return true
	}
	for _, allowed := range rs {
		if allowed == role {
			return true
		}
	}
	return false
}

// Names devuelve los roles como texto, para mensajes de error legibles.
func (rs RoleSet) Names() []string {
	names := make([]string, 0, len(rs))
	for _, r := range rs {
		names = append(names, string(r))
	}
	return names
}
