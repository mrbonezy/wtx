package main

import "testing"

func TestShouldFetchByBranch(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		loadedKey   string
		fetchingKey string
		want        bool
	}{
		{name: "new key", key: "a", loadedKey: "", fetchingKey: "", want: true},
		{name: "loaded key", key: "a", loadedKey: "a", fetchingKey: "", want: false},
		{name: "fetching key", key: "a", loadedKey: "", fetchingKey: "a", want: false},
		{name: "empty key", key: "", loadedKey: "", fetchingKey: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldFetchByBranch(tc.key, tc.loadedKey, tc.fetchingKey)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}
