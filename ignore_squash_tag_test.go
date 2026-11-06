package mapstructure

import (
	"reflect"
	"testing"
)

// Tests for IgnoreSquashTag, a behaviour this fork adds to upstream. In a file
// of its own for the reason explicit_nil_values_test.go gives: a rebase onto a
// new upstream release stays cheap, and the option cannot be dropped by a bad
// merge while the upstream suite still passes.
//
// # What the option is for
//
// A decoder that reads the tag another library writes from. With TagName
// "json", a field tagged `json:"meta,squash"` is read flat by mapstructure,
// while encoding/json, which does not know the option, writes it nested under
// "meta": one tag, two shapes. With the option the field is read under its
// own name, as encoding/json reads and writes it. Squash still squashes an
// embedded struct, which encoding/json flattens too.

type squashTagMeta struct {
	Tags []string `json:"tags"`
}

type squashTagNamed struct {
	Name string        `json:"name"`
	Meta squashTagMeta `json:"meta,squash"`
}

type squashTagNamedPtr struct {
	Name string         `json:"name"`
	Meta *squashTagMeta `json:"meta,squash"`
}

type squashTagEmbedded struct {
	Name          string `json:"name"`
	squashTagMeta `json:"meta,squash"`
}

type squashTagCustom struct {
	Meta squashTagMeta `json:"meta,inline"`
}

func decodeSquashTag(t *testing.T, ignore bool, option string, input, result any) *Metadata {
	t.Helper()
	var md Metadata
	d, err := NewDecoder(&DecoderConfig{
		TagName:         "json",
		Squash:          true,
		SquashTagOption: option,
		IgnoreSquashTag: ignore,
		Metadata:        &md,
		Result:          result,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Decode(input); err != nil {
		t.Fatal(err)
	}
	return &md
}

func TestIgnoreSquashTag_ReadsANamedFieldUnderItsName(t *testing.T) {
	var got squashTagNamed
	md := decodeSquashTag(t, true, "", map[string]any{"name": "n", "meta": map[string]any{"tags": []string{"a"}}}, &got)
	if want := (squashTagNamed{Name: "n", Meta: squashTagMeta{Tags: []string{"a"}}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("nested: got %+v, want %+v", got, want)
	}
	if len(md.Unused) != 0 {
		t.Fatalf("nested: unused %v", md.Unused)
	}

	got = squashTagNamed{}
	md = decodeSquashTag(t, true, "", map[string]any{"name": "n", "tags": []string{"a"}}, &got)
	if got.Meta.Tags != nil {
		t.Fatalf("flat: the field was squashed: %+v", got)
	}
	if !reflect.DeepEqual(md.Unused, []string{"tags"}) {
		t.Fatalf("flat: unused %v, want [tags]", md.Unused)
	}
}

func TestIgnoreSquashTag_ReadsANamedPointerUnderItsName(t *testing.T) {
	var got squashTagNamedPtr
	decodeSquashTag(t, true, "", map[string]any{"meta": map[string]any{"tags": []string{"a"}}}, &got)
	if got.Meta == nil || !reflect.DeepEqual(got.Meta.Tags, []string{"a"}) {
		t.Fatalf("got %+v", got)
	}
}

// Unset, the option changes nothing: the tag still squashes, as upstream, so
// the nested form is the one left unused.
func TestIgnoreSquashTag_UnsetKeepsTheTag(t *testing.T) {
	var got squashTagNamed
	decodeSquashTag(t, false, "", map[string]any{"name": "n", "tags": []string{"a"}}, &got)
	if !reflect.DeepEqual(got.Meta.Tags, []string{"a"}) {
		t.Fatalf("flat: got %+v", got)
	}
	got = squashTagNamed{}
	md := decodeSquashTag(t, false, "", map[string]any{"meta": map[string]any{"tags": []string{"a"}}}, &got)
	if got.Meta.Tags != nil || !reflect.DeepEqual(md.Unused, []string{"meta"}) {
		t.Fatalf("nested: got %+v, unused %v, want [meta]", got, md.Unused)
	}
}

// Squash still squashes an embedded struct, tag or no tag.
func TestIgnoreSquashTag_KeepsSquashForAnEmbeddedStruct(t *testing.T) {
	var got squashTagEmbedded
	decodeSquashTag(t, true, "", map[string]any{"name": "n", "tags": []string{"a"}}, &got)
	if !reflect.DeepEqual(got.Tags, []string{"a"}) {
		t.Fatalf("got %+v", got)
	}
}

// It ignores the option whatever SquashTagOption names it.
func TestIgnoreSquashTag_IgnoresACustomOption(t *testing.T) {
	var got squashTagCustom
	decodeSquashTag(t, true, "inline", map[string]any{"meta": map[string]any{"tags": []string{"a"}}}, &got)
	if !reflect.DeepEqual(got.Meta.Tags, []string{"a"}) {
		t.Fatalf("got %+v", got)
	}
}

// From a struct to a map, the field is written nested, as encoding/json
// writes it; unset, it is squashed into the map, as upstream.
func TestIgnoreSquashTag_WritesANamedFieldNested(t *testing.T) {
	in := squashTagNamed{Name: "n", Meta: squashTagMeta{Tags: []string{"a"}}}
	for _, c := range []struct {
		ignore bool
		want   map[string]any
	}{
		{true, map[string]any{"name": "n", "meta": map[string]any{"tags": []string{"a"}}}},
		{false, map[string]any{"name": "n", "tags": []string{"a"}}},
	} {
		got := map[string]any{}
		decodeSquashTag(t, c.ignore, "", in, &got)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("ignore=%v: got %#v, want %#v", c.ignore, got, c.want)
		}
	}
}
