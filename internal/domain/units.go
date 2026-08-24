package domain

import "math"

// family agrupa unidades que pueden convertirse entre sí
func family(u Unit) (string, bool) {
	switch u {
	case UnitGram, UnitKilogram:
		return "mass", true
	case UnitMilliliter, UnitLiter:
		return "volume", true
	case UnitPiece:
		return "piece", true
	default:
		return "", false
	}
}

// baseFactor devuelve cuántas unidades base equivalen a una unidad u
func baseFactor(u Unit) float64 {
	switch u {
	case UnitKilogram, UnitLiter:
		return 1000
	default: // UnitGram, UnitMilliliter, UnitPiece
		return 1
	}
}

// ToBaseUnits convierte quantity a la unidad base del ingrediente, redondeando a un entero
func ToBaseUnits(quantity float64, from Unit, base Unit) (int64, error) {
	fromFamily, ok := family(from)
	if !ok {
		return 0, ErrIncompatibleUnit
	}
	baseFamily, ok := family(base)
	if !ok {
		return 0, ErrIncompatibleUnit
	}
	if fromFamily != baseFamily {
		return 0, ErrIncompatibleUnit
	}
	// La unidad base de un ingrediente siempre debe ser la unidad "pequeña"
	// de su familia (g, ml o unit)
	converted := quantity * baseFactor(from)
	return int64(math.Round(converted)), nil
}
