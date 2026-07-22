// Package validate wraps go-playground/validator with the custom rules this
// service needs (Bangladeshi phone numbers, date strings) and turns validation
// failures into a clean field->message map for JSON error responses.
package validate

import (
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// bdPhoneRe matches Bangladeshi mobile numbers in the common forms:
//
//	01XXXXXXXXX            (11 digits, local)
//	+8801XXXXXXXXX         (E.164)
//	008801XXXXXXXXX
//
// where the operator digit after 01 is 3–9.
var bdPhoneRe = regexp.MustCompile(`^(?:\+?880|0)1[3-9]\d{8}$`)

// imgDataRe matches a base64 image data-URI (the inline passport photo).
var imgDataRe = regexp.MustCompile(`^data:image/(jpeg|jpg|png|webp);base64,[A-Za-z0-9+/]+=*$`)

type Validator struct {
	v *validator.Validate
}

func New() *Validator {
	v := validator.New()

	_ = v.RegisterValidation("bdphone", func(fl validator.FieldLevel) bool {
		raw := strings.ReplaceAll(fl.Field().String(), " ", "")
		raw = strings.ReplaceAll(raw, "-", "")
		return bdPhoneRe.MatchString(raw)
	})

	// datestr accepts ISO (2006-01-02) or dd-mm-yyyy / dd/mm/yyyy so the form
	// can send either without a 400.
	_ = v.RegisterValidation("datestr", func(fl validator.FieldLevel) bool {
		s := strings.TrimSpace(fl.Field().String())
		for _, layout := range []string{"2006-01-02", "02-01-2006", "02/01/2006"} {
			if _, err := time.Parse(layout, s); err == nil {
				return true
			}
		}
		return false
	})

	// imgdata accepts a base64-encoded image data-URI (jpeg/png/webp). The
	// passport photo is uploaded inline and encrypted at rest; this bounds it to
	// a real image payload before it reaches the cipher/DB.
	_ = v.RegisterValidation("imgdata", func(fl validator.FieldLevel) bool {
		return imgDataRe.MatchString(strings.TrimSpace(fl.Field().String()))
	})

	return &Validator{v: v}
}

// Struct validates s and returns a field->message map (nil if valid).
func (val *Validator) Struct(s any) map[string]string {
	err := val.v.Struct(s)
	if err == nil {
		return nil
	}
	out := make(map[string]string)
	var verrs validator.ValidationErrors
	if ok := asValidationErrors(err, &verrs); !ok {
		out["_"] = "invalid input"
		return out
	}
	for _, fe := range verrs {
		out[strings.ToLower(fe.Field())] = message(fe)
	}
	return out
}

func asValidationErrors(err error, target *validator.ValidationErrors) bool {
	if verrs, ok := err.(validator.ValidationErrors); ok {
		*target = verrs
		return true
	}
	return false
}

func message(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "this field is required"
	case "email":
		return "must be a valid email address"
	case "bdphone":
		return "must be a valid Bangladeshi mobile number"
	case "datestr":
		return "must be a valid date"
	case "min":
		return "too short"
	case "max":
		return "too long"
	case "len":
		return "wrong length"
	case "numeric":
		return "must be a number"
	case "oneof":
		return "must be one of the allowed values"
	case "url":
		return "must be a valid URL"
	case "imgdata":
		return "must be a valid image"
	default:
		return "invalid value"
	}
}
