package clojure_lang

/*
#cgo CFLAGS: -I${SRCDIR}/../clojure/src -std=c11
#include "parser.c"
*/
import "C"

import (
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"
)

func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_clojure())
	return sitter.NewLanguage(ptr)
}
