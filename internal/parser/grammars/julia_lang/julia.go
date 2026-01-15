package julia_lang

/*
#cgo CFLAGS: -I${SRCDIR}/../julia/src -std=c11 -fPIC
#include "parser.c"
#include "scanner.c"
*/
import "C"

import (
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"
)

func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_julia())
	return sitter.NewLanguage(ptr)
}
