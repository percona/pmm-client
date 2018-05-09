package service

import "testing"

func TestTemplateFunctionCmd(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{
			"bad'p$ass",
			"bad'p$ass",
			`'bad'"'"'p$ass'`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tf["cmd"].(func(string) string)(tt.arg); got != tt.want {
				t.Errorf("tf[cmd](%s) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}
