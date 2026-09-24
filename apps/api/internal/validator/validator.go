// Package validator configures go-playground/validator for Gin-compatible
// validation and exposes a singleton Validate.
package validator

import (
	"reflect"
	"sync"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

var (
	once     sync.Once
	validate *validator.Validate
)

// Get returns the singleton validator.Validate instance, registering it with
// Gin's binding layer so `binding:"..."` tags use it.
func Get() *validator.Validate {
	once.Do(func() {
		if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
			validate = v
			// Use JSON field names in error output.
			validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
				if name := fld.Tag.Get("json"); name != "" {
					if name == "-" {
						return ""
					}
					if idx := indexByte(name, ','); idx >= 0 {
						return name[:idx]
					}
					return name
				}
				return fld.Name
			})
		} else {
			validate = validator.New()
		}
	})
	return validate
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
