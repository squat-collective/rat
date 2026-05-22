package main

import "testing"

func TestChartCRUD(t *testing.T) {
	s := newStore()
	c := s.createChart(&Chart{Title: "Sales", Type: "bar", SQL: "SELECT 1", XColumn: "x", YColumns: []string{"y"}})
	if c.ID == "" {
		t.Fatal("created chart should have an ID")
	}
	if c.CreatedAt.IsZero() {
		t.Error("created chart should have a CreatedAt timestamp")
	}
	got, ok := s.getChart(c.ID)
	if !ok || got.Title != "Sales" {
		t.Fatalf("getChart returned %v, %v", got, ok)
	}
	if len(s.listCharts()) != 1 {
		t.Errorf("expected 1 chart, got %d", len(s.listCharts()))
	}
	if !s.deleteChart(c.ID) {
		t.Error("deleteChart should report success")
	}
	if _, ok := s.getChart(c.ID); ok {
		t.Error("chart should be gone after delete")
	}
	if s.deleteChart("missing") {
		t.Error("deleting a missing chart should report failure")
	}
}

func TestDashboardUpdateAndAddWidget(t *testing.T) {
	s := newStore()
	d := s.createDashboard(&Dashboard{Title: "Ops"})
	if d.Widgets == nil {
		t.Error("a new dashboard should have a non-nil widget slice")
	}

	// Rename only — widgets must be left untouched.
	title := "Ops v2"
	upd, ok := s.updateDashboard(d.ID, &title, nil)
	if !ok || upd.Title != "Ops v2" {
		t.Fatalf("rename failed: %v, %v", upd, ok)
	}
	if !upd.UpdatedAt.After(d.CreatedAt) && !upd.UpdatedAt.Equal(d.CreatedAt) {
		t.Error("UpdatedAt should advance on update")
	}

	// Replace widgets.
	widgets := []Widget{{ChartID: "chart-1", Width: 2, Height: 1}}
	upd, ok = s.updateDashboard(d.ID, nil, &widgets)
	if !ok || len(upd.Widgets) != 1 {
		t.Fatalf("widget replace failed: %v", upd)
	}
	if upd.Title != "Ops v2" {
		t.Error("title should be preserved when only widgets are updated")
	}

	if _, ok := s.updateDashboard("missing", &title, nil); ok {
		t.Error("updating a missing dashboard should report failure")
	}
}

func TestReportCRUD(t *testing.T) {
	s := newStore()
	rep := s.createReport(&Report{
		Title: "Quarterly",
		Blocks: []ReportBlock{
			{Kind: "text", Text: "# Summary"},
			{Kind: "chart", ChartID: "chart-1"},
		},
	})
	if rep.ID == "" || len(rep.Blocks) != 2 {
		t.Fatalf("unexpected report: %v", rep)
	}
	got, ok := s.getReport(rep.ID)
	if !ok || got.Title != "Quarterly" {
		t.Fatalf("getReport returned %v, %v", got, ok)
	}
	if len(s.listReports()) != 1 {
		t.Errorf("expected 1 report, got %d", len(s.listReports()))
	}
	if !s.deleteReport(rep.ID) {
		t.Error("deleteReport should report success")
	}
}

func TestIDsAreUnique(t *testing.T) {
	s := newStore()
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := s.id("chart")
		if seen[id] {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = true
	}
}
