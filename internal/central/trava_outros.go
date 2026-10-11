//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package central

import "context"

// travarInicio não tem flock nesta plataforma: a primeira abertura concorrente de um banco novo
// pode falhar com banco_ocupado e basta repetir. O orq roda em Linux (o Herdr é só Linux).
func travarInicio(context.Context, string) (func(), error) { return func() {}, nil }
