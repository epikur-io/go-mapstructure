package mapstructure

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Tests for ExplicitNilValues, a behaviour this fork adds to upstream (see also
// ignore_squash_tag_test.go and name_embedded_by_tag_test.go).
//
// It lives in its own file on purpose. The feature is ten lines in
// mapstructure.go and everything else here tracks upstream, so rebasing onto a
// new upstream release is meant to be cheap - and a test appended to
// mapstructure_test.go would conflict on every one of those rebases. More
// importantly, without a test the ten lines can be dropped by a bad merge and
// the entire upstream suite still passes, which is exactly how a fork loses the
// feature it exists for.
//
// # What the flag is for
//
// A PATCH endpoint that loads a row and decodes a partial body onto it. Three
// cases have to be told apart, and only the flag distinguishes the second:
//
//	key absent          -> leave the field alone
//	key present, null   -> clear the field
//	key present, value  -> assign it
//
// Without it, a client asking to clear a column is answered "ok" and nothing
// happens.

type envSettings struct {
	Name    string            `json:"name"`
	Owner   *envOwner         `json:"owner"`
	Comment *string           `json:"comment"`
	Labels  map[string]string `json:"labels"`
	Hosts   []string          `json:"hosts"`
}

type envOwner struct {
	ID   int    `json:"id"`
	Team string `json:"team"`
}

func ptrTo[T any](v T) *T { return &v }

// storedSettings is a row as it came out of a database: every field populated,
// so that anything a decode fails to preserve is visible.
func storedSettings() *envSettings {
	return &envSettings{
		Name:    "production",
		Owner:   &envOwner{ID: 7, Team: "platform"},
		Comment: ptrTo("keep an eye on this one"),
		Labels:  map[string]string{"tier": "1", "region": "eu"},
		Hosts:   []string{"a", "b"},
	}
}

func TestDecoder_ExplicitNilValuesOption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  map[string]any
		expect func(*envSettings) error
	}{
		{
			// The case the flag exists for.
			name:  "explicit null clears a leaf pointer",
			input: map[string]any{"comment": nil},
			expect: func(s *envSettings) error {
				if s.Comment != nil {
					return errf("comment = %q, want nil", *s.Comment)
				}
				if s.Name != "production" {
					return errf("name = %q, want it untouched", s.Name)
				}
				return nil
			},
		},
		{
			// The other half of the same rule, and the half that is easy to get
			// backwards: absent is not null.
			name:  "an absent key leaves the field alone",
			input: map[string]any{"name": "staging"},
			expect: func(s *envSettings) error {
				if s.Comment == nil {
					return errf("comment was cleared by a body that never mentioned it")
				}
				if s.Name != "staging" {
					return errf("name = %q, want %q", s.Name, "staging")
				}
				return nil
			},
		},
		{
			// The pointer itself is cleared. Upstream unwraps every non-nil
			// pointer-to-struct field before decoding into it, so a null
			// zeroed the struct behind the pointer and left the pointer
			// non-nil; this fork unwraps embedded fields only.
			name:  "explicit null clears a pointer to a struct",
			input: map[string]any{"owner": nil},
			expect: func(s *envSettings) error {
				if s.Owner != nil {
					return errf("owner = %+v, want nil", *s.Owner)
				}
				return nil
			},
		},
		{
			// Merging into a nested struct is upstream behaviour and works
			// with or without the flag. Pinned anyway: it is half of what the
			// PATCH endpoints promise, and a regression here is silent.
			name:  "a partial nested object merges",
			input: map[string]any{"owner": map[string]any{"team": "infra"}},
			expect: func(s *envSettings) error {
				if s.Owner == nil {
					return errf("owner was cleared by a partial update")
				}
				if s.Owner.Team != "infra" {
					return errf("owner.team = %q, want %q", s.Owner.Team, "infra")
				}
				if s.Owner.ID != 7 {
					return errf("owner.id = %d, want 7 - the field was not in the body", s.Owner.ID)
				}
				return nil
			},
		},
		{
			// The case that separates this flag from ZeroFields, which is the
			// obvious-looking substitute for it. ZeroFields makes a fresh map
			// and drops "region"; this keeps it. Anyone tempted to replace the
			// flag with ZeroFields fails here and nowhere else.
			name:  "a partial map merges rather than replacing",
			input: map[string]any{"labels": map[string]any{"tier": "2"}},
			expect: func(s *envSettings) error {
				if got := s.Labels["tier"]; got != "2" {
					return errf("labels[tier] = %q, want %q", got, "2")
				}
				if got := s.Labels["region"]; got != "eu" {
					return errf("labels[region] = %q, want it untouched - "+
						"ZeroFields would drop it, ExplicitNilValues must not", got)
				}
				return nil
			},
		},
		{
			name:  "explicit null clears a map",
			input: map[string]any{"labels": nil},
			expect: func(s *envSettings) error {
				if len(s.Labels) != 0 {
					return errf("labels = %v, want empty", s.Labels)
				}
				return nil
			},
		},
		{
			name:  "explicit null clears a slice",
			input: map[string]any{"hosts": nil},
			expect: func(s *envSettings) error {
				if len(s.Hosts) != 0 {
					return errf("hosts = %v, want empty", s.Hosts)
				}
				return nil
			},
		},
		{
			name:  "an ordinary value still decodes",
			input: map[string]any{"comment": "replaced"},
			expect: func(s *envSettings) error {
				if s.Comment == nil || *s.Comment != "replaced" {
					return errf("comment = %v, want %q", s.Comment, "replaced")
				}
				return nil
			},
		},
	}

	for _, test := range tests {
		// go.mod declares go 1.18, so the pre-1.22 loop semantics apply and
		// `test` is one shared variable. Harmless while the subtest runs
		// inline, fatal the moment anyone adds t.Parallel() to it - every case
		// would then read the last entry and the table would assert one thing
		// eight times. Upstream's tables avoid this by not going parallel; this
		// takes the belt as well.
		test := test
		t.Run(test.name, func(t *testing.T) {
			result := storedSettings()
			decoder, err := NewDecoder(&DecoderConfig{
				TagName:           "json",
				ExplicitNilValues: true,
				Result:            result,
			})
			if err != nil {
				t.Fatalf("err: %s", err)
			}
			if err := decoder.Decode(test.input); err != nil {
				t.Fatalf("err: %s", err)
			}
			if err := test.expect(result); err != nil {
				t.Fatalf("bad: %s", err)
			}
		})
	}
}

// Without the flag, a null is indistinguishable from an absent key. This is the
// behaviour the fork exists to change, asserted so that the difference is
// recorded rather than implied.
func TestDecoder_WithoutExplicitNilValuesNullIsIgnored(t *testing.T) {
	t.Parallel()

	result := storedSettings()
	decoder, err := NewDecoder(&DecoderConfig{
		TagName: "json",
		Result:  result,
	})
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if err := decoder.Decode(map[string]any{"comment": nil}); err != nil {
		t.Fatalf("err: %s", err)
	}

	if result.Comment == nil {
		t.Fatal("bad: upstream cleared the field on an explicit null; " +
			"if this now holds, ExplicitNilValues may be redundant and the fork can go")
	}
	if *result.Comment != "keep an eye on this one" {
		t.Fatalf("bad: %q", *result.Comment)
	}
}

// The flag stands on its own: DecodeNil is about running the hook on a nil
// input, and callers should not have to set it to get explicit nils.
func TestDecoder_ExplicitNilValuesIsIndependentOfDecodeNil(t *testing.T) {
	t.Parallel()

	for _, decodeNil := range []bool{false, true} {
		result := storedSettings()
		decoder, err := NewDecoder(&DecoderConfig{
			TagName:           "json",
			ExplicitNilValues: true,
			DecodeNil:         decodeNil,
			Result:            result,
		})
		if err != nil {
			t.Fatalf("err: %s", err)
		}
		if err := decoder.Decode(map[string]any{"comment": nil}); err != nil {
			t.Fatalf("err: %s", err)
		}
		if result.Comment != nil {
			t.Fatalf("bad: DecodeNil=%v left comment set to %q", decodeNil, *result.Comment)
		}
	}
}

// A nil pointer field is still filled in by a body that supplies one, so the
// flag does not turn "clear" into "cannot set".
func TestDecoder_ExplicitNilValuesStillFillsNilPointers(t *testing.T) {
	t.Parallel()

	result := &envSettings{Name: "production"}
	decoder, err := NewDecoder(&DecoderConfig{
		TagName:           "json",
		ExplicitNilValues: true,
		Result:            result,
	})
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if err := decoder.Decode(map[string]any{
		"owner":   map[string]any{"id": 3, "team": "data"},
		"comment": "new",
	}); err != nil {
		t.Fatalf("err: %s", err)
	}

	if result.Owner == nil {
		t.Fatal("bad: owner is nil")
	}
	if !reflect.DeepEqual(*result.Owner, envOwner{ID: 3, Team: "data"}) {
		t.Fatalf("bad: %+v", *result.Owner)
	}
	if result.Comment == nil || *result.Comment != "new" {
		t.Fatalf("bad: %v", result.Comment)
	}
}

// A pointer to any struct is cleared by a null, a time.Time included, at any
// depth; an object sent for one still merges; and an embedded pointer still
// flattens, its fields read from the outer keys.
func TestDecoder_ExplicitNilValuesClearsStructPointers(t *testing.T) {
	t.Parallel()

	type account struct {
		ID        int        `json:"id"`
		ExpiresAt *time.Time `json:"expires_at"`
		Owner     *envOwner  `json:"owner"`
	}
	type withEmbedded struct {
		*envOwner
		Name string `json:"name"`
	}
	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	decode := func(t *testing.T, input map[string]any, result any) {
		t.Helper()
		decoder, err := NewDecoder(&DecoderConfig{
			TagName:           "json",
			Squash:            true,
			ExplicitNilValues: true,
			DecodeHook:        StringToTimeHookFunc(time.RFC3339),
			Result:            result,
		})
		if err != nil {
			t.Fatalf("err: %s", err)
		}
		if err := decoder.Decode(input); err != nil {
			t.Fatalf("err: %s", err)
		}
	}

	t.Run("a null clears a pointer to a time", func(t *testing.T) {
		got := &account{ID: 1, ExpiresAt: ptrTo(expires), Owner: &envOwner{ID: 7, Team: "platform"}}
		decode(t, map[string]any{"expires_at": nil}, got)
		if got.ExpiresAt != nil {
			t.Fatalf("expires_at = %v, want nil", *got.ExpiresAt)
		}
		if got.ID != 1 || got.Owner == nil || *got.Owner != (envOwner{ID: 7, Team: "platform"}) {
			t.Fatalf("bad: the rest moved: %+v %+v", got, got.Owner)
		}
	})

	t.Run("a time is still assigned", func(t *testing.T) {
		got := &account{ID: 1, ExpiresAt: ptrTo(expires)}
		decode(t, map[string]any{"expires_at": "2031-01-01T00:00:00Z"}, got)
		if got.ExpiresAt == nil || !got.ExpiresAt.Equal(time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("expires_at = %v", got.ExpiresAt)
		}
	})

	t.Run("a null one level down, under an object that merges", func(t *testing.T) {
		type holder struct {
			Account *account `json:"account"`
		}
		got := &holder{Account: &account{ID: 1, ExpiresAt: ptrTo(expires), Owner: &envOwner{ID: 7}}}
		decode(t, map[string]any{"account": map[string]any{"owner": nil}}, got)
		if got.Account == nil || got.Account.ID != 1 || got.Account.ExpiresAt == nil {
			t.Fatalf("bad: the merged object lost fields: %+v", got.Account)
		}
		if got.Account.Owner != nil {
			t.Fatalf("owner = %+v, want nil", *got.Account.Owner)
		}
	})

	t.Run("an embedded pointer still flattens", func(t *testing.T) {
		got := &withEmbedded{envOwner: &envOwner{ID: 7, Team: "platform"}, Name: "n"}
		decode(t, map[string]any{"team": "infra"}, got)
		if got.envOwner == nil || *got.envOwner != (envOwner{ID: 7, Team: "infra"}) || got.Name != "n" {
			t.Fatalf("bad: %+v %+v", got.envOwner, got)
		}
	})
}

// errf keeps the table's expectations to one line each; the message is what a
// failing case reports.
func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
