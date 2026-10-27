package model

import (
	"strings"
	"testing"
	"time"
)

func TestMetaSettingsNormalizeAndValidate(t *testing.T) {
	in := MetaSettingsInput{Config: MetaConfig{
		DatasetID: " 123456789 ", AppID: " 445566778899 ", AdAccountID: "act_987654321", PageID: "1122334455",
		FormIDs:          []string{"55555", " 55555", "", "66666"},
		CAPIEnabled:      true,
		TestMode:         true,
		TestEventCode:    "TEST4321",
		PhoneCountryCode: "+91",
		EventMapping:     []StageEvent{{Stage: StageQualified, EventName: " Qualified Lead "}},
	}}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		t.Fatalf("valid settings refused: %v", errs)
	}
	c := in.Config
	if c.AppID != "445566778899" || c.AdAccountID != "987654321" || c.DatasetID != "123456789" || c.PhoneCountryCode != "91" {
		t.Errorf("not normalised: %+v", c)
	}
	if len(c.FormIDs) != 2 || c.EventMapping[0].EventName != "Qualified Lead" {
		t.Errorf("forms/mapping = %v / %v", c.FormIDs, c.EventMapping)
	}
}

func TestMetaSettingsRefusals(t *testing.T) {
	in := MetaSettingsInput{
		Config: MetaConfig{
			PageID: "my-page", AppID: "app-123", FormIDs: []string{"12"},
			CAPIEnabled: true, TestMode: true, // no dataset, no test code
			EventMapping: []StageEvent{
				{Stage: StageNew, EventName: "New"},
				{Stage: StageWon, EventName: "Won!"},
				{Stage: StageWon, EventName: "Won"},
				{Stage: StageLost, EventName: "won"},
			},
		},
		Secrets: MetaSecrets{CAPIToken: "has space"},
	}
	in.Normalize()
	errs := in.Validate()
	for _, key := range []string{
		"config.page_id", "config.app_id", "config.form_ids", "config.dataset_id", "config.test_event_code",
		"config.event_mapping[1].event_name",
		"config.event_mapping[2].stage", "config.event_mapping[3].event_name", "secrets.capi_token",
	} {
		if errs[key] == "" {
			t.Errorf("no error for %s (got %v)", key, errs)
		}
	}
}

func TestMetaSecretsMergeKeepsBlankFields(t *testing.T) {
	saved := MetaSecrets{CAPIToken: "old-capi", PageAccessToken: "old-page", AppSecret: "s", VerifyToken: "v"}
	got := saved.Merge(MetaSecrets{PageAccessToken: "new-page"})
	if got.CAPIToken != "old-capi" || got.PageAccessToken != "new-page" || got.AppSecret != "s" {
		t.Errorf("merge = %+v", got)
	}
}

func TestEventsReachingBackfillsEarlierStages(t *testing.T) {
	c := DefaultMetaConfig() // new -> Lead, qualified -> Qualified Lead, won -> Converted
	names := func(evs []StageEvent) string {
		var out []string
		for _, e := range evs {
			out = append(out, e.EventName)
		}
		return strings.Join(out, ",")
	}
	// A lead jumping straight to Won sends every earlier mapped stage too.
	if got := names(c.EventsReaching(StageWon)); got != "Lead,Qualified Lead,Converted" {
		t.Errorf("won = %s", got)
	}
	if got := names(c.EventsReaching(StageContacted)); got != "Lead" {
		t.Errorf("contacted = %s", got)
	}
	// Lost is off the main path.
	c.EventMapping = append(c.EventMapping, StageEvent{Stage: StageLost, EventName: "Disqualified"})
	if got := names(c.EventsReaching(StageLost)); got != "Disqualified" {
		t.Errorf("lost = %s", got)
	}
	// A disabled mapping is skipped, and its name kept.
	c.EventMapping[1].Disabled = true
	if got := names(c.EventsReaching(StageWon)); got != "Lead,Converted" {
		t.Errorf("won with qualified disabled = %s", got)
	}
	if _, ok := c.EventFor(StageQualified); ok {
		t.Error("a disabled mapping still sends")
	}
}

func TestEventMappingLookup(t *testing.T) {
	c := DefaultMetaConfig()
	if name, ok := c.EventFor(StageQualified); !ok || name != "Qualified Lead" {
		t.Errorf("qualified -> %q %v", name, ok)
	}
	if _, ok := c.EventFor(StageContacted); ok {
		t.Error("an unmapped stage sends an event")
	}
	if !c.TestMode || c.CAPIEnabled {
		t.Error("defaults must start in test mode with CAPI off")
	}
}

func TestCAPIEventIDIsStableAndModeSpecific(t *testing.T) {
	a := CAPIEventID(42, "Qualified Lead", false)
	if a != "crm-42-qualified-lead-live" || a != CAPIEventID(42, "Qualified Lead", false) {
		t.Errorf("event id = %q", a)
	}
	if CAPIEventID(42, "Qualified Lead", true) == a {
		t.Error("test and live events share an id")
	}
}

func TestCAPIBackoff(t *testing.T) {
	if got := CAPIBackoff(1, 0); got != time.Minute {
		t.Errorf("first = %v", got)
	}
	if got := CAPIBackoff(99, 0); got != 6*time.Hour {
		t.Errorf("cap = %v", got)
	}
	if got := CAPIBackoff(1, time.Hour); got != time.Hour {
		t.Errorf("Retry-After ignored: %v", got)
	}
}
