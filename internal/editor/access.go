package editor

import (
	"github.com/egoist/mygo/ui"
	"reflect"
)

// MyGo 0.7.4 exposes a lazy Access callback but keeps its node type internal.
// Infer that callback's type and set only its exported Value field. This keeps
// screen readers' full text current without building it on every visual frame;
// no unsafe access, private fields or changes to MyGo's module are involved.
func accessTextValue[T any](access func(func(T)) ui.Element, text func() string) {
	access(func(node T) { reflect.ValueOf(node).Elem().FieldByName("Value").SetString(text()) })
}
