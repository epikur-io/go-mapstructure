package mapstructure

import (
	"reflect"
	"testing"
)

// Tests for NameEmbeddedByTag, a behaviour this fork adds to upstream. In
// a file of its own for the reason explicit_nil_values_test.go gives.
//
// # What the option is for
//
// encoding/json flattens an embedded struct only when its tag gives no name;
// one embedded as `Audit `json:"audit"“ is a field named "audit", written
// nested. Squash squashes every embedded struct, named or not, so such a type
// was read flat and written nested. With the option, an embedded struct whose
// tag gives a name is decoded as that named field, in both directions; one
// without a name is still squashed.

type namedEmbedAudit struct {
	CreatedBy string `json:"created_by"`
}

type namedEmbedBook struct {
	namedEmbedAudit `json:"audit"`
	Title           string `json:"title"`
}

// NamedEmbedAudit is exported: a pointer to an unexported embedded type
// cannot be allocated through reflection, by encoding/json either.
type NamedEmbedAudit struct {
	CreatedBy string `json:"created_by"`
}

type namedEmbedBookPtr struct {
	*NamedEmbedAudit `json:"audit"`
	Title            string `json:"title"`
}

type unnamedEmbedBook struct {
	namedEmbedAudit `json:",omitempty"`
	Title           string `json:"title"`
}

type namedEmbedSquashTag struct {
	namedEmbedAudit `json:"audit,squash"`
}

type namedEmbedConfig struct {
	named, ignoreSquashTag, explicitNil bool
}

func decodeNamedEmbed(t *testing.T, c namedEmbedConfig, input, result any) *Metadata {
	t.Helper()
	var md Metadata
	d, err := NewDecoder(&DecoderConfig{
		TagName:           "json",
		Squash:            true,
		NameEmbeddedByTag: c.named,
		IgnoreSquashTag:   c.ignoreSquashTag,
		ExplicitNilValues: c.explicitNil,
		DecodeNil:         c.explicitNil,
		Metadata:          &md,
		Result:            result,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Decode(input); err != nil {
		t.Fatal(err)
	}
	return &md
}

func TestNameEmbeddedByTag_ReadsANamedEmbeddedStructUnderItsName(t *testing.T) {
	on := namedEmbedConfig{named: true}
	var got namedEmbedBook
	md := decodeNamedEmbed(t, on, map[string]any{"title": "t", "audit": map[string]any{"created_by": "ada"}}, &got)
	if got.CreatedBy != "ada" || got.Title != "t" || len(md.Unused) != 0 {
		t.Fatalf("nested: got %+v, unused %v", got, md.Unused)
	}

	got = namedEmbedBook{}
	md = decodeNamedEmbed(t, on, map[string]any{"title": "t", "created_by": "ada"}, &got)
	if got.CreatedBy != "" || !reflect.DeepEqual(md.Unused, []string{"created_by"}) {
		t.Fatalf("flat: got %+v, unused %v, want [created_by]", got, md.Unused)
	}
}

// A named embedded pointer is a named pointer field: an object sets it, and a
// null clears it under ExplicitNilValues, as for any named pointer.
func TestNameEmbeddedByTag_ReadsANamedEmbeddedPointerAsAField(t *testing.T) {
	c := namedEmbedConfig{named: true, explicitNil: true}
	var got namedEmbedBookPtr
	decodeNamedEmbed(t, c, map[string]any{"audit": map[string]any{"created_by": "ada"}}, &got)
	if got.NamedEmbedAudit == nil || got.CreatedBy != "ada" {
		t.Fatalf("object: got %+v", got)
	}

	decodeNamedEmbed(t, c, map[string]any{"audit": nil}, &got)
	if got.NamedEmbedAudit != nil {
		t.Fatalf("null: got %+v, want a nil pointer", got)
	}
}

// Unset, the option changes nothing: Squash squashes the named embedded struct,
// as upstream, so the nested form is the one left unused.
func TestNameEmbeddedByTag_UnsetKeepsSquash(t *testing.T) {
	var got namedEmbedBook
	decodeNamedEmbed(t, namedEmbedConfig{}, map[string]any{"created_by": "ada"}, &got)
	if got.CreatedBy != "ada" {
		t.Fatalf("flat: got %+v", got)
	}
	got = namedEmbedBook{}
	md := decodeNamedEmbed(t, namedEmbedConfig{}, map[string]any{"audit": map[string]any{"created_by": "ada"}}, &got)
	if got.CreatedBy != "" || !reflect.DeepEqual(md.Unused, []string{"audit"}) {
		t.Fatalf("nested: got %+v, unused %v, want [audit]", got, md.Unused)
	}
}

// An embedded struct whose tag gives no name is still squashed.
func TestNameEmbeddedByTag_KeepsSquashForAnUnnamedEmbeddedStruct(t *testing.T) {
	var got unnamedEmbedBook
	decodeNamedEmbed(t, namedEmbedConfig{named: true}, map[string]any{"created_by": "ada", "title": "t"}, &got)
	if got.CreatedBy != "ada" {
		t.Fatalf("got %+v", got)
	}
}

// An explicit squash option keeps its meaning, which IgnoreSquashTag governs.
func TestNameEmbeddedByTag_LeavesTheSquashOptionToIgnoreSquashTag(t *testing.T) {
	var got namedEmbedSquashTag
	decodeNamedEmbed(t, namedEmbedConfig{named: true}, map[string]any{"created_by": "ada"}, &got)
	if got.CreatedBy != "ada" {
		t.Fatalf("option honoured: got %+v", got)
	}
	got = namedEmbedSquashTag{}
	decodeNamedEmbed(t, namedEmbedConfig{named: true, ignoreSquashTag: true}, map[string]any{"audit": map[string]any{"created_by": "ada"}}, &got)
	if got.CreatedBy != "ada" {
		t.Fatalf("option ignored: got %+v", got)
	}
}

type namedEmbedExportedBook struct {
	NamedEmbedAudit `json:"audit"`
	Title           string `json:"title"`
}

// From a struct to a map, a named embedded struct is written nested, as
// encoding/json writes it; unset, it is squashed into the map, as upstream.
// An exported type: upstream writes no field of an unexported embedded type
// to a map at all, named or not.
func TestNameEmbeddedByTag_WritesANamedEmbeddedStructNested(t *testing.T) {
	in := namedEmbedExportedBook{NamedEmbedAudit: NamedEmbedAudit{CreatedBy: "ada"}, Title: "t"}
	for _, c := range []struct {
		named bool
		want  map[string]any
	}{
		{true, map[string]any{"title": "t", "audit": map[string]any{"created_by": "ada"}}},
		{false, map[string]any{"title": "t", "created_by": "ada"}},
	} {
		got := map[string]any{}
		decodeNamedEmbed(t, namedEmbedConfig{named: c.named}, in, &got)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("named=%v: got %#v, want %#v", c.named, got, c.want)
		}
	}
}
