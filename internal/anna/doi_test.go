package anna

import "testing"

func TestPaperTitleFromPageTitle(t *testing.T) {
	t.Parallel()

	if got := paperTitleFromPageTitle("A Paper - Anna's Archive"); got != "A Paper" {
		t.Fatalf("paperTitleFromPageTitle returned %q", got)
	}
	if got := paperTitleFromPageTitle("Unrecognized title"); got != "" {
		t.Fatalf("paperTitleFromPageTitle returned %q for unmatched title", got)
	}
}

func TestJournalFromDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		want        string
	}{
		{description: "Authors\n\nPublisher\n\nJournal, volume, year", want: "Journal, volume, year"},
		{description: "Authors\n\nPublisher", want: "Publisher"},
		{description: "Journal only", want: "Journal only"},
	}
	for _, test := range tests {
		if got := journalFromDescription(test.description); got != test.want {
			t.Errorf("journalFromDescription(%q) = %q, want %q", test.description, got, test.want)
		}
	}
}
