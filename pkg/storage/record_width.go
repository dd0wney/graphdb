package storage

import (
	"errors"
	"fmt"
	"math"
)

// maxRecordFieldWidth is the largest count or byte length the mmap snapshot
// format can record for a property count, a property key, a label count, a
// label, a tenant ID, an edge type, or a membership key: it writes each as a
// uint16.
const maxRecordFieldWidth = math.MaxUint16

// ErrRecordTooWide is returned when a node or edge has a count or a length
// the snapshot format cannot record. The write is refused before it reaches
// memory, in either snapshot mode, so that a store never holds a record one
// of its own formats would corrupt.
var ErrRecordTooWide = errors.New("record field exceeds the snapshot format's limit")

// checkWidth refuses a count or a byte length above maxRecordFieldWidth.
func checkWidth(field string, n int) error {
	if n > maxRecordFieldWidth {
		return fmt.Errorf("%w: %s is %d, the limit is %d", ErrRecordTooWide, field, n, maxRecordFieldWidth)
	}
	return nil
}

// checkMembershipKey refuses a label or edge type whose membership key,
// kind byte + tenant + 0x00 + name (membFullKey), is too long. Each part can be
// within the limit while the key is not. The key holds the effective tenant:
// a record with tenant "" is keyed under "default".
func checkMembershipKey(field, tenant, name string) error {
	keyTenant := effectiveTenantID(tenant).String()
	return checkWidth(field+" with its tenant ID (membership key)", 2+len(keyTenant)+len(name))
}

func checkPropertyWidths(props map[string]Value) error {
	if err := checkWidth("property count", len(props)); err != nil {
		return err
	}
	for k := range props {
		if err := checkWidth("property key length", len(k)); err != nil {
			return err
		}
	}
	return nil
}

// checkNodeWidths refuses a node the snapshot format cannot record. tenant is
// the node's TenantID as stored; the membership check resolves it.
func checkNodeWidths(tenant string, labels []string, props map[string]Value) error {
	if err := checkWidth("tenant ID length", len(tenant)); err != nil {
		return err
	}
	if err := checkWidth("label count", len(labels)); err != nil {
		return err
	}
	for _, l := range labels {
		if err := checkWidth("label length", len(l)); err != nil {
			return err
		}
		if err := checkMembershipKey("label", tenant, l); err != nil {
			return err
		}
	}
	return checkPropertyWidths(props)
}

// checkEdgeWidths refuses an edge the snapshot format cannot record.
func checkEdgeWidths(tenant, edgeType string, props map[string]Value) error {
	if err := checkWidth("tenant ID length", len(tenant)); err != nil {
		return err
	}
	if err := checkWidth("edge type length", len(edgeType)); err != nil {
		return err
	}
	if err := checkMembershipKey("edge type", tenant, edgeType); err != nil {
		return err
	}
	return checkPropertyWidths(props)
}

// checkPatchWidths refuses a patch whose new keys are too long or whose
// result has too many properties. It counts the merged map without building
// it, because it runs on every update.
func checkPatchWidths(existing, set map[string]Value, remove []string) error {
	count := len(existing)
	for k := range set {
		if err := checkWidth("property key length", len(k)); err != nil {
			return err
		}
		if _, ok := existing[k]; !ok {
			count++
		}
	}
	if len(remove) == 0 {
		return checkWidth("property count", count)
	}
	removed := make(map[string]struct{}, len(remove))
	for _, k := range remove {
		if _, dup := removed[k]; dup {
			continue
		}
		removed[k] = struct{}{}
		_, had := existing[k]
		_, setToo := set[k]
		if had || setToo {
			count-- // a key in both is set and then removed
		}
	}
	return checkWidth("property count", count)
}
