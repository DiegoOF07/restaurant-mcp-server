package httpmcp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

/*
Límite de peticiones por cliente, con el algoritmo de token bucket
*/

// bucket es la cubeta de un cliente.
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// rateLimiter reparte cubetas por clave de cliente.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // fichas por segundo
	burst   float64 // capacidad máxima
}

// newRateLimiter crea el limitador. Un rate de 0 o menos lo deja desactivado (devuelve nil,
// que Allow trata como "todo permitido").
func newRateLimiter(rate, burst float64) *rateLimiter {
	if rate <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{buckets: make(map[string]*bucket), rate: rate, burst: burst}
}

// Allow descuenta una ficha de la cubeta del cliente. Devuelve false si no quedaban, junto
// con cuánto hay que esperar para que se reponga una.
func (l *rateLimiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		// Un cliente nuevo empieza con la cubeta llena menos la petición que acaba de hacer.
		l.buckets[key] = &bucket{tokens: l.burst - 1, lastSeen: now}
		return true, 0
	}

	l.refill(b, now)

	if b.tokens < 1 {
		// Cuánto falta para tener una ficha entera.
		wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
		return false, wait
	}

	b.tokens--
	return true, 0
}

// refill acredita las fichas correspondientes al tiempo transcurrido, sin pasar del máximo.
// Quien lo llama debe tener el lock tomado.
func (l *rateLimiter) refill(b *bucket, now time.Time) {
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens = minFloat(l.burst, b.tokens+elapsed*l.rate)
	b.lastSeen = now
}

// cleanup descarta las cubetas de clientes que llevan rato sin aparecer
func (l *rateLimiter) cleanup(idleFor time.Duration) {
	if l == nil {
		return
	}

	now := time.Now()
	cutoff := now.Add(-idleFor)

	l.mu.Lock()
	defer l.mu.Unlock()
	for key, b := range l.buckets {
		if !b.lastSeen.Before(cutoff) {
			continue
		}

		l.refill(b, now)

		if b.tokens >= l.burst {
			delete(l.buckets, key)
		}
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

/*
clientKey identifica al cliente para efectos del límite
*/
func clientKey(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// El primero de la lista es el cliente original; el resto son los proxies.
			if first, _, found := strings.Cut(xff, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(xff)
		}
		if real := r.Header.Get("X-Real-IP"); real != "" {
			return strings.TrimSpace(real)
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
