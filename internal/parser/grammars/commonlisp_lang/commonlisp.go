package commonlisp_lang

/*
#cgo CFLAGS: -I${SRCDIR}/../commonlisp/src -std=c11
#include "parser.c"
*/
import "C"

import (
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"
)

func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_commonlisp())
	return sitter.NewLanguage(ptr)
}
