package dsl

import "strings"

const lexerSymbolRunes = "{}[]:=+-*/()?<>|&,!"

func isSymbolRune(ch rune) bool {
	return strings.ContainsRune(lexerSymbolRunes, ch)
}
