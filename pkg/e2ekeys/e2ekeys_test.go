package e2ekeys

import "testing"

func TestContactCode(t *testing.T) {
	a := ContactCode("usera", "masterA", "userb", "masterB")
	if a != ContactCode("userb", "masterB", "usera", "masterA") {
		t.Fatal("the two sides see different codes")
	}
	if a == ContactCode("usera", "masterA", "userb", "masterC") {
		t.Fatal("another master key gives the same code")
	}
	// Vector shared with the app (client/src/e2e/keys.test.ts).
	if want := "23CS-JAL6-535R-CJ5Z-MAVW-7WT2"; a != want {
		t.Fatalf("code %s, want %s", a, want)
	}
}
