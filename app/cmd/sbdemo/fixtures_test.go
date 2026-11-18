package main

import (
	"testing"
	"time"
)

func TestWeekly(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 18, 14, 0, 0, 0, loc) // a Wednesday
	f := &fixer{loc: loc, now: now, end: now.AddDate(0, 0, 56)}
	tue := f.weekly(time.Tuesday, 19, 0, 1, 0)
	if len(tue) != 8 || tue[0].Format("2006-01-02 15:04") != "2026-11-24 19:00" {
		t.Fatalf("Tuesdays: %v", tue)
	}
	for _, d := range tue {
		if d.Weekday() != time.Tuesday || d.Hour() != 19 || holiday(d) {
			t.Fatalf("bad date %v", d)
		}
	}
	// Today's own evening counts; Christmas doesn't.
	if wed := f.weekly(time.Wednesday, 20, 30, 1, 0); wed[0].Day() != 18 {
		t.Fatalf("Wednesdays: %v", wed)
	}
	fri := f.weekly(time.Friday, 20, 30, 1, 0)
	for _, d := range fri {
		if d.Month() == time.December && d.Day() == 25 {
			t.Fatal("no open mic on Christmas Day")
		}
	}
	// Every other week: the two halves share no date and cover them all
	// (but the holidays).
	a, b := f.weekly(time.Friday, 20, 30, 2, 0), f.weekly(time.Friday, 20, 30, 2, 1)
	if len(a)+len(b) != len(fri) {
		t.Fatalf("%d + %d Fridays, want %d", len(a), len(b), len(fri))
	}
	// Another run, a day later, finds the same dates.
	g := &fixer{loc: loc, now: now.AddDate(0, 0, 1), end: now.AddDate(0, 0, 57)}
	if g.weekly(time.Friday, 20, 30, 2, 0)[0] != a[0] {
		t.Fatal("the open mic weeks should not move")
	}
	if thirdSaturday(2027, time.March, loc).Day() != 20 {
		t.Fatal("thirdSaturday")
	}
}
