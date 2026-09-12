package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// SettingField is one configurable value.
type SettingField struct {
	// Key is the environment variable it is stored as.
	Key string
	// Label is what the learner reads.
	Label string
	// Help says what it unlocks, in terms of what the app can then do.
	Help string
	// Secret hides the value and never shows it back.
	Secret bool
	// Value is the current setting. For a secret this is only used to decide
	// whether something is set; it is never rendered.
	Value string
	// Section groups fields under a heading.
	Section string
	// RestartRequired marks a field the running session cannot pick up.
	RestartRequired bool
}

// Configured reports whether the field has a value.
func (f SettingField) Configured() bool { return strings.TrimSpace(f.Value) != "" }

// SettingsConfig wires the settings dialog to the host.
type SettingsConfig struct {
	// Fields are rendered in order, grouped by Section.
	Fields func() []SettingField
	// Save persists the changed values and applies what it can. It returns a
	// one-line summary of what took effect, which the dialog shows.
	Save func(changed map[string]string) (string, error)
}

// openSettings shows the configuration dialog.
//
// Secrets are write-only here: an existing key is shown as "set" and never
// rendered back, so the window can be open while someone is watching a demo.
// Leaving a field blank keeps the stored value rather than clearing it, which
// is the behaviour that cannot lose someone's key by accident.
func (s *Shell) openSettings() {
	if s.cfg.Settings.Fields == nil {
		return
	}
	fields := s.cfg.Settings.Fields()
	entries := make(map[string]*widget.Entry, len(fields))

	var rows []fyne.CanvasObject
	section := ""
	for _, f := range fields {
		if f.Section != section {
			if section != "" {
				rows = append(rows, widget.NewSeparator())
			}
			section = f.Section
			rows = append(rows, sectionLabel(strings.ToUpper(section)))
		}

		entry := widget.NewEntry()
		if f.Secret {
			entry = widget.NewPasswordEntry()
		}
		if f.Configured() {
			if f.Secret {
				entry.SetPlaceHolder("set — type to replace, leave blank to keep")
			} else {
				entry.SetText(f.Value)
			}
		} else {
			entry.SetPlaceHolder("not set")
		}
		entries[f.Key] = entry

		rows = append(rows, container.NewVBox(
			container.NewBorder(nil, nil, nil, statusTag(s.pal, f), body(f.Label)),
			entry,
			muted(f.Help),
		))
	}

	form := container.NewVScroll(container.New(layoutPadding{h: 6, v: 6}, container.NewVBox(rows...)))
	form.SetMinSize(fyne.NewSize(620, 560))

	d := dialog.NewCustomConfirm("Settings", "Save", "Cancel", form, func(ok bool) {
		if !ok || s.cfg.Settings.Save == nil {
			return
		}
		changed := map[string]string{}
		for _, f := range fields {
			v := strings.TrimSpace(entries[f.Key].Text)
			if v == "" || v == f.Value {
				continue // blank keeps what is stored
			}
			changed[f.Key] = v
		}
		if len(changed) == 0 {
			return
		}

		summary, err := s.cfg.Settings.Save(changed)
		if err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		s.feed.Add(container.NewPadded(accented(s.pal.surface, s.pal.success, 8, body(summary))))
		s.bump()
	}, s.win)
	d.Resize(fyne.NewSize(700, 660))
	d.Show()
}

// statusTag says whether a field is live, in the same colours the sidebar uses
// for held versus owed.
func statusTag(p palette, f SettingField) fyne.CanvasObject {
	switch {
	case !f.Configured():
		return tag("not set", theme.ColorNamePlaceHolder, fyne.TextStyle{})
	case f.RestartRequired:
		return tag("set · restart to change", theme.ColorNamePlaceHolder, fyne.TextStyle{})
	default:
		return tag("set", theme.ColorNameSuccess, fyne.TextStyle{Bold: true})
	}
}

// settingsButton is the header control that opens the dialog.
func (s *Shell) settingsButton() fyne.CanvasObject {
	if s.cfg.Settings.Fields == nil {
		return layoutBlank()
	}
	b := widget.NewButton("Settings", s.openSettings)
	return b
}

// missingKeysNote lists capabilities that are switched off for want of a key,
// so the gap is visible before someone wonders why a tool never fires.
func missingKeysNote(fields []SettingField) string {
	var missing []string
	for _, f := range fields {
		if f.Section == "Keys" && !f.Configured() {
			missing = append(missing, f.Label)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("%s — off, no key", strings.Join(missing, ", "))
}
