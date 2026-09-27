package config

import "testing"

func TestClampInt(t *testing.T) {
	cases := []struct {
		name  string
		value int
		min   int
		max   int
		want  int
	}{
		{name: "低于下界取下界", value: 5, min: 10, max: 300, want: 10},
		{name: "高于上界取上界", value: 999, min: 10, max: 300, want: 300},
		{name: "区间内原样返回", value: 180, min: 10, max: 300, want: 180},
		{name: "等于下界", value: 10, min: 10, max: 300, want: 10},
		{name: "等于上界", value: 300, min: 10, max: 300, want: 300},
		{name: "允许下界为零", value: -1, min: 0, max: 3, want: 0},
		{name: "上下界相同", value: 7, min: 5, max: 5, want: 5},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := clampInt(testCase.value, testCase.min, testCase.max); got != testCase.want {
				t.Fatalf("clampInt(%d, %d, %d) = %d, want %d", testCase.value, testCase.min, testCase.max, got, testCase.want)
			}
		})
	}
}
