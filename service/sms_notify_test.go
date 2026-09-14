package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidSMSPhoneNumber(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "mainland mobile", value: "13800138000", valid: true},
		{name: "international E164", value: "+14155552671", valid: true},
		{name: "short number", value: "12345", valid: false},
		{name: "formatted number", value: "138-0013-8000", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.valid, ValidSMSPhoneNumber(test.value))
		})
	}
}
