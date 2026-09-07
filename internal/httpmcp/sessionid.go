package httpmcp

import (
	"crypto/rand"
	"encoding/hex"
)

// newSessionID genera el identificador de una sesión.
//
// Se usa crypto/rand y no math/rand porque este identificador es, de hecho, una credencial:
// quien lo adivine podría intentar usar una sesión ajena. 128 bits de entropía hacen
// inviable adivinarlo, y además se comprueba que la sesión pertenezca a la misma identidad.
//
// La especificación exige que sea visible sólo con caracteres ASCII imprimibles; el
// hexadecimal lo cumple de sobra.
func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand sólo falla si el sistema no tiene fuente de entropía, situación en la
		// que seguir sirviendo sería peor que caerse.
		panic("httpmcp: no hay fuente de entropía disponible: " + err.Error())
	}
	return hex.EncodeToString(buf)
}
