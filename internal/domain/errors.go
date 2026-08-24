package domain

import "errors"

// Errores de NEGOCIO. Se distinguen de los errores de PROTOCOLO (método
// desconocido, parámetros malformados)
var (
	ErrDishNotFound          = errors.New("dish not found")
	ErrIngredientNotFound    = errors.New("ingredient not found")
	ErrIncompatibleUnit      = errors.New("incompatible unit for this ingredient")
	ErrInsufficientInventory = errors.New("insufficient inventory for this operation")
	ErrInvalidOperation      = errors.New("invalid inventory operation")
	ErrInvalidServings       = errors.New("servings must be greater than zero")
)
