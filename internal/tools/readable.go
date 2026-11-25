package tools

import (
	"strconv"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
)

// readableQuantity muestra una cantidad en unidad base como la leería alguien en cocina:
// 1200 g -> "1.2 kg", 2000 ml -> "2 l", 3 unit -> "3 unidades".
func readableQuantity(q int64, u domain.Unit) string {
	switch u {
	case domain.UnitGram:
		if q >= 1000 {
			return decimal(float64(q)/1000) + " kg"
		}
		return strconv.FormatInt(q, 10) + " g"
	case domain.UnitMilliliter:
		if q >= 1000 {
			return decimal(float64(q)/1000) + " l"
		}
		return strconv.FormatInt(q, 10) + " ml"
	case domain.UnitPiece:
		if q == 1 {
			return "1 unidad"
		}
		return strconv.FormatInt(q, 10) + " unidades"
	default:
		return strconv.FormatInt(q, 10) + " " + string(u)
	}
}

func decimal(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// unitName nombra la unidad base en palabras, para los mensajes de unidad incompatible.
func unitName(u domain.Unit) string {
	switch u {
	case domain.UnitGram, domain.UnitKilogram:
		return "gramos"
	case domain.UnitMilliliter, domain.UnitLiter:
		return "mililitros"
	case domain.UnitPiece:
		return "unidades"
	default:
		return string(u)
	}
}

func roleName(r domain.Role) string {
	switch r {
	case domain.RoleWaiter:
		return "Mesero"
	case domain.RoleCook:
		return "Cocina"
	case domain.RoleAdmin:
		return "Administración"
	default:
		return string(r)
	}
}

func toolAction(name string) string {
	if name == "adjust_inventory" {
		return "ajustar el inventario"
	}
	return "usar esta herramienta"
}

// unitLabel nombra la unidad que pidió el usuario; solo "unit" necesita traducción
func unitLabel(u string) string {
	if u == string(domain.UnitPiece) {
		return "unidades"
	}
	return u
}
