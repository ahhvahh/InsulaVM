package registry

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestInsertVMReportsDuplicateIdentity(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	original := Entry{
		ID: "019a5d11-1234-7123-8123-123456789abc",
		LibvirtUUID: "019a5d11-1234-7123-8123-123456789abc",
		Name: "insula-unique-name",
		Profile: "alpine", MAC: "52:54:00:11:22:33", InstanceDir: "/tmp/original",
	}
	if err := store.Insert(ctx, original); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Entry)
		want string
	}{
		{"duplicate ID", func(e *Entry) {
			e.ID = original.ID
			e.LibvirtUUID = "019a5d11-1234-7123-8123-123456789abd"
			e.Name = "insula-other"
		}, "UUID"},
		{"duplicate Libvirt UUID", func(e *Entry) {
			e.ID = "019a5d11-1234-7123-8123-123456789abd"
			e.LibvirtUUID = original.LibvirtUUID
			e.Name = "insula-other"
		}, "Libvirt UUID"},
		{"duplicate name", func(e *Entry) {
			e.ID = "019a5d11-1234-7123-8123-123456789abd"
			e.LibvirtUUID = e.ID
			e.Name = original.Name
		}, "name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := original
			tc.edit(&copy)
			err := store.Insert(ctx, copy)
			if !errors.Is(err, ErrVMAlreadyExists) || !strings.Contains(err.Error(), tc.want) ||
				!strings.Contains(err.Error(), "already exists") {
				t.Fatalf("expected clear duplicate identity error, got %v", err)
			}
			entries, listErr := store.List(ctx)
			if listErr != nil || len(entries) != 1 || entries[0].ID != original.ID {
				t.Fatalf("failed insert changed registered VM: %+v %v", entries, listErr)
			}
		})
	}
}

func TestInsertVMRejectsDeletedUUIDWithExplanation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	entry := Entry{
		ID: "019a5d11-1234-7123-8123-123456789abc",
		LibvirtUUID: "019a5d11-1234-7123-8123-123456789abc",
		Name: "insula-test", Profile: "alpine",
		MAC: "52:54:00:11:22:33", InstanceDir: "/tmp/original",
	}
	if err := store.Insert(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	err = store.Insert(ctx, entry)
	if !errors.Is(err, ErrVMAlreadyExists) || !strings.Contains(err.Error(), "previously used") {
		t.Fatalf("previously deleted UUID must remain reserved: %v", err)
	}
}
