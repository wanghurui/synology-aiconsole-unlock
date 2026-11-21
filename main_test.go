package main

import (
	"bytes"
	"testing"
)

func TestSignatureIsUniqueInStockLibrary(t *testing.T) {
	// A 4-byte patch site appears in plenty of places; the 8-byte signature
	// including the adjacent activation_time store must be unique. This test
	// pins that property against a synthetic buffer that mimics the real
	// function body around 0x631f4.
	body := []byte{
		0x48, 0xc7, 0x44, 0x24, 0x08, 0x00, 0x00, 0x00, // mov qword ptr [rsp+8], 0
		0xc6, 0x44, 0x24, 0x10, 0x00, // mov byte ptr [rsp+0x10], 0
		0x40, 0x88, 0x6b, 0x20, // mov byte ptr [rbx+0x20], bpl
		0x4c, 0x89, 0x73, 0x28, // mov qword ptr [rbx+0x28], r14
		0x45, 0x84, 0xe4, 0x75, 0x47, // test r12b, r12b / jne
	}
	if got := bytes.Count(body, sigUnpatched); got != 1 {
		t.Fatalf("unpatched signature matched %d times, want 1", got)
	}
	if got := bytes.Count(body, sigPatched); got != 0 {
		t.Fatalf("patched signature matched %d times in stock body, want 0", got)
	}
}

func TestPatchReplacesOnlyTheStore(t *testing.T) {
	orig := []byte{
		0xc6, 0x44, 0x24, 0x10, 0x00,
		0x40, 0x88, 0x6b, 0x20,
		0x4c, 0x89, 0x73, 0x28,
		0x45, 0x84, 0xe4,
	}
	want := []byte{
		0xc6, 0x44, 0x24, 0x10, 0x00,
		0xc6, 0x43, 0x20, 0x01,
		0x4c, 0x89, 0x73, 0x28,
		0x45, 0x84, 0xe4,
	}

	idx := bytes.Index(orig, sigUnpatched)
	if idx != 5 {
		t.Fatalf("signature found at %d, want 5", idx)
	}

	patched := make([]byte, len(orig))
	copy(patched, orig)
	copy(patched[idx:], patchBytes)

	if !bytes.Equal(patched, want) {
		t.Fatalf("patched body mismatch\n got %x\nwant %x", patched, want)
	}
	if len(patched) != len(orig) {
		t.Fatalf("patch changed length: %d -> %d", len(orig), len(patched))
	}
	if got := bytes.Count(patched, sigPatched); got != 1 {
		t.Fatalf("patched signature matched %d times, want 1", got)
	}
	if got := bytes.Count(patched, sigUnpatched); got != 0 {
		t.Fatalf("unpatched signature still matched %d times, want 0", got)
	}
}

func TestSignatureDoesNotMatchUnrelatedBytes(t *testing.T) {
	// Four bytes of the store alone are not distinctive; make sure a buffer
	// holding only that fragment is not mistaken for a patch site.
	fragment := []byte{0x40, 0x88, 0x6b, 0x20, 0x00, 0x00, 0x00, 0x00}
	if bytes.Count(fragment, sigUnpatched) != 0 {
		t.Fatal("four-byte fragment must not satisfy the eight-byte signature")
	}
	if bytes.Count(fragment, sigPatched) != 0 {
		t.Fatal("four-byte fragment must not satisfy the patched signature")
	}
}

func TestPatchIsSameLength(t *testing.T) {
	if len(patchBytes) != 4 {
		t.Fatalf("patch is %d bytes, want 4", len(patchBytes))
	}
	if len(sigUnpatched) != len(sigPatched) {
		t.Fatalf("signature lengths differ: %d vs %d", len(sigUnpatched), len(sigPatched))
	}
	// The patched signature must be the unpatched one with only the first
	// four bytes swapped, otherwise the trailing context would not survive.
	if !bytes.Equal(sigUnpatched[4:], sigPatched[4:]) {
		t.Fatalf("trailing context differs: %x vs %x", sigUnpatched[4:], sigPatched[4:])
	}
	if !bytes.Equal(sigPatched[:4], patchBytes) {
		t.Fatalf("patched signature prefix %x != patch bytes %x", sigPatched[:4], patchBytes)
	}
}
