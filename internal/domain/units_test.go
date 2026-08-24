package domain

import "testing"

func TestToBaseUnits_SameUnit(t *testing.T) {
	got, err := ToBaseUnits(80, UnitGram, UnitGram)
	if err != nil {
		t.Fatalf("no esperaba error: %v", err)
	}
	if got != 80 {
		t.Errorf("esperaba 80, obtuve %d", got)
	}
}

func TestToBaseUnits_KilogramToGram(t *testing.T) {
	got, err := ToBaseUnits(2, UnitKilogram, UnitGram)
	if err != nil {
		t.Fatalf("no esperaba error: %v", err)
	}
	if got != 2000 {
		t.Errorf("esperaba 2000, obtuve %d", got)
	}
}

func TestToBaseUnits_LiterToMilliliter(t *testing.T) {
	got, err := ToBaseUnits(1.5, UnitLiter, UnitMilliliter)
	if err != nil {
		t.Fatalf("no esperaba error: %v", err)
	}
	if got != 1500 {
		t.Errorf("esperaba 1500, obtuve %d", got)
	}
}

func TestToBaseUnits_IncompatibleFamily(t *testing.T) {
	_, err := ToBaseUnits(1, UnitGram, UnitMilliliter)
	if err != ErrIncompatibleUnit {
		t.Fatalf("esperaba ErrIncompatibleUnit, obtuve %v", err)
	}
}

func TestToBaseUnits_PieceMismatch(t *testing.T) {
	_, err := ToBaseUnits(1, UnitPiece, UnitGram)
	if err != ErrIncompatibleUnit {
		t.Fatalf("esperaba ErrIncompatibleUnit, obtuve %v", err)
	}
}

func TestToBaseUnits_RoundsToInteger(t *testing.T) {
	got, err := ToBaseUnits(0.3333, UnitKilogram, UnitGram)
	if err != nil {
		t.Fatalf("no esperaba error: %v", err)
	}
	if got != 333 {
		t.Errorf("esperaba redondeo a 333, obtuve %d", got)
	}
}