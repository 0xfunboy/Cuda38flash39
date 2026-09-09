package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestOwnershipRejectReplacement(t *testing.T) {
	u := OwnedUnit{Rank: 0, Name: "strixglm-rank0", Nonce: strings.Repeat("a", 32), InvocationID: strings.Repeat("b", 32)}
	m := map[string]string{"ActiveState": "active", "MainPID": "22", "InvocationID": strings.Repeat("c", 32), "Environment": "CIRU_OWNER_NONCE=" + u.Nonce}
	if verifyUnit(u, m) == nil {
		t.Fatal("foreign InvocationID accepted")
	}
	m["InvocationID"] = u.InvocationID
	if e := verifyUnit(u, m); e != nil {
		t.Fatal(e)
	}
	m["Environment"] = "CIRU_OWNER_NONCE=" + u.Nonce + "x"
	if verifyUnit(u, m) == nil {
		t.Fatal("substring nonce accepted")
	}
}
func TestWholePairPrevalidation(t *testing.T) {
	var mu sync.Mutex
	stops := 0
	us := []OwnedUnit{{Rank: 0, Name: "strixglm-rank0", Nonce: strings.Repeat("a", 32), InvocationID: strings.Repeat("b", 32)}, {Rank: 1, Name: "strixglm-rank1", Nonce: strings.Repeat("c", 32), InvocationID: strings.Repeat("d", 32)}}
	c := &ClusterController{}
	c.run = func(ctx context.Context, rank int, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if args[2] == "stop" {
			stops++
			return nil, nil
		}
		u := us[rank]
		id := u.InvocationID
		if rank == 1 {
			id = strings.Repeat("e", 32)
		}
		return []byte(fmt.Sprintf("ActiveState=active\nMainPID=22\nInvocationID=%s\nEnvironment=CIRU_OWNER_NONCE=%s\n", id, u.Nonce)), nil
	}
	if c.stopUnits(context.Background(), us, false) == nil {
		t.Fatal("foreign rank accepted")
	}
	if stops != 0 {
		t.Fatal("stopped a rank before verifying peer")
	}
}
func TestPairStopBoth(t *testing.T) {
	var mu sync.Mutex
	stopped := [2]bool{}
	us := []OwnedUnit{{Rank: 0, Name: "strixglm-rank0", Nonce: strings.Repeat("a", 32), InvocationID: strings.Repeat("b", 32)}, {Rank: 1, Name: "strixglm-rank1", Nonce: strings.Repeat("c", 32), InvocationID: strings.Repeat("d", 32)}}
	c := &ClusterController{}
	c.run = func(ctx context.Context, rank int, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if args[2] == "stop" {
			stopped[rank] = true
			return nil, nil
		}
		u := us[rank]
		if stopped[rank] {
			return []byte("ActiveState=inactive\nMainPID=0\n"), nil
		}
		return []byte(fmt.Sprintf("ActiveState=active\nMainPID=22\nInvocationID=%s\nEnvironment=CIRU_OWNER_NONCE=%s\n", u.InvocationID, u.Nonce)), nil
	}
	if e := c.stopUnits(context.Background(), us, false); e != nil {
		t.Fatal(e)
	}
	if !stopped[0] || !stopped[1] {
		t.Fatal("both ranks were not stopped")
	}
}
func TestControllerLockRejectsConcurrentAction(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pair.lock")
	if e := withControllerLock(p, func() error {
		if withControllerLock(p, func() error { return nil }) == nil {
			return errors.New("concurrent lock accepted")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestSystemdArgvPreservesEmptyArgument(t *testing.T) {
	v, e := parseSystemdQuoted(`"/usr/bin/bash" "/path with spaces/run.sh" "0" "" "1"`)
	if e != nil || len(v) != 5 || v[3] != "" || v[1] != "/path with spaces/run.sh" {
		t.Fatalf("%v %v", v, e)
	}
	if _, e = parseSystemdQuoted(`bash -c arbitrary`); e == nil {
		t.Fatal("unquoted input accepted")
	}
}
func TestControllerDurableState(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if e := controllerWrite(p, map[string]string{"state": "starting"}); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p)
	if e != nil || !strings.Contains(string(b), "starting") {
		t.Fatal("missing saved intent")
	}
}
func TestOwnedBindingRejectsRankAPI(t *testing.T) {
	if validUnits([]OwnedUnit{{Rank: 0, Name: "ciru-model-rank1-001", Nonce: strings.Repeat("a", 32)}}, true) == nil {
		t.Fatal("wrong-node binding accepted")
	}
}

func legacyRecoveryFixture(t *testing.T) (*ClusterController, string, string, func() int, func()) {
	t.Helper()
	base := t.TempDir()
	cfg := ClusterConfig{StateDir: filepath.Join(base, "product-state"), EngineRoot: filepath.Join(base, "engine"), Launcher: filepath.Join(base, "launch.sh"), Manifest: filepath.Join(base, "manifest.json"), LegacyOwner: filepath.Join(base, "engine/state/pair-owner.json")}
	c, e := NewClusterController(cfg)
	if e != nil {
		t.Fatal(e)
	}
	units := []OwnedUnit{{Rank: 1, Name: "ciru-model-rank1-001", Nonce: strings.Repeat("a", 32), InvocationID: strings.Repeat("b", 32)}, {Rank: 0, Name: "ciru-model-rank0-001", Nonce: strings.Repeat("c", 32), InvocationID: strings.Repeat("d", 32)}, {Rank: 0, Name: "ciru-frontend-001", Nonce: strings.Repeat("e", 32), InvocationID: strings.Repeat("f", 32)}}
	if e = controllerWrite(cfg.LegacyOwner, map[string]any{"units": units, "epoch": "1788860560452", "state": "promoted", "depth": 5, "local_draft": 0, "safe_prefill": true, "canonical_moe": true}); e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	stopped := map[string]bool{}
	stops := 0
	foreign := false
	c.run = func(ctx context.Context, rank int, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) < 4 || args[0] != "systemctl" {
			return nil, fmt.Errorf("unexpected command %v", args)
		}
		name := args[3]
		var u OwnedUnit
		found := false
		for _, x := range units {
			if x.Name == name {
				u = x
				found = true
			}
		}
		if !found {
			return nil, errors.New("unknown unit")
		}
		switch args[2] {
		case "show":
			if stopped[name] {
				return []byte("ActiveState=inactive\nMainPID=0\n"), nil
			}
			inv := u.InvocationID
			if foreign && u.Rank == 1 {
				inv = strings.Repeat("9", 32)
			}
			return []byte(fmt.Sprintf("ActiveState=active\nMainPID=12\nInvocationID=%s\nEnvironment=CIRU_OWNER_NONCE=%s\n", inv, u.Nonce)), nil
		case "cat":
			argv := []string{"/usr/bin/bash", filepath.Join(cfg.EngineRoot, "scripts/run-rank.sh"), strconv.Itoa(rank), "5", "1788860560452", "0", "", "1", "1", "0"}
			memory := "124554051584"
			if name == "ciru-frontend-001" {
				argv = []string{filepath.Join(cfg.EngineRoot, "venv/bin/python"), filepath.Join(cfg.EngineRoot, "scripts/frontend.py"), "--poison-marker", filepath.Join(filepath.Dir(cfg.LegacyOwner), "frontend-poison.json")}
				memory = "2147483648"
			}
			quoted := []string{}
			for _, arg := range argv {
				quoted = append(quoted, strconv.Quote(arg))
			}
			return []byte("[Service]\nMemoryMax=" + memory + "\nStandardOutput=append:" + filepath.Join(base, "reports/legacy", name+".log") + "\nExecStart=" + strings.Join(quoted, " ") + "\n"), nil
		case "stop":
			stopped[name] = true
			stops++
			return nil, nil
		}
		return nil, errors.New("unexpected unit operation")
	}
	snapshot := filepath.Join(base, "snapshot.json")
	if e = c.SnapshotLegacy(context.Background(), snapshot); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(filepath.Dir(cfg.LegacyOwner), "frontend-poison.json")
	if e = os.WriteFile(marker, []byte(`{"reason":"rank1 RemoteProtocolError"}`), 0600); e != nil {
		t.Fatal(e)
	}
	return c, snapshot, marker, func() int { mu.Lock(); defer mu.Unlock(); return stops }, func() { mu.Lock(); foreign = true; mu.Unlock() }
}
func TestExplicitLegacyRecoveryPreservesPoisonUntilWholeStop(t *testing.T) {
	c, snapshot, marker, stops, _ := legacyRecoveryFixture(t)
	if e := c.StopLegacy(context.Background(), snapshot); e == nil {
		t.Fatal("ordinary stop accepted poison")
	}
	if stops() != 0 {
		t.Fatal("ordinary stop mutated services")
	}
	if e := c.RecoverLegacy(context.Background(), snapshot); e != nil {
		t.Fatal(e)
	}
	if stops() != 3 {
		t.Fatalf("not all3 units stopped: %d", stops())
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("marker not archived")
	}
	raw, e := filepath.Glob(filepath.Join(c.Config.StateDir, "legacy-recovery-intent-*.json"))
	if e != nil || len(raw) != 1 {
		t.Fatal("missing durable raw intent")
	}
	archived, e := filepath.Glob(filepath.Join(c.Config.StateDir, "legacy-poison-recovered-*.json"))
	if e != nil || len(archived) != 1 {
		t.Fatal("missing poison archive")
	}
	b, e := os.ReadFile(archived[0])
	if e != nil || !strings.Contains(string(b), "RemoteProtocolError") {
		t.Fatal("poison evidence changed")
	}
}
func TestExplicitLegacyRecoveryRejectsForeignPeerAndKeepsPoison(t *testing.T) {
	c, snapshot, marker, stops, replace := legacyRecoveryFixture(t)
	replace()
	if e := c.RecoverLegacy(context.Background(), snapshot); e == nil {
		t.Fatal("foreign peer accepted")
	}
	if stops() != 0 {
		t.Fatal("stopped owned rank before rejecting foreign peer")
	}
	if _, e := os.Stat(marker); e != nil {
		t.Fatal("poison removed despite no proven whole-pair stop")
	}
}

func stoppedLegacyRestoreFixture(t *testing.T) (*ClusterController, string, LegacySnapshot, map[string]any) {
	t.Helper()
	c, path, _, _, _ := legacyRecoveryFixture(t)
	s, e := loadLegacySnapshot(path)
	if e != nil {
		t.Fatal(e)
	}
	var owner map[string]any
	if e = json.Unmarshal(s.Owner, &owner); e != nil {
		t.Fatal(e)
	}
	owner["state"] = "stopped"
	owner["strixglm_snapshot"] = path
	if e = controllerWrite(c.Config.LegacyOwner, owner); e != nil {
		t.Fatal(e)
	}
	return c, path, s, owner
}

func TestRestoreLegacyForeignStoppedOwnerHasZeroActions(t *testing.T) {
	for _, field := range []string{"nonce", "invocation_id", "epoch", "missing_unit", "state", "snapshot"} {
		t.Run(field, func(t *testing.T) {
			c, path, _, owner := stoppedLegacyRestoreFixture(t)
			units := owner["units"].([]any)
			switch field {
			case "nonce", "invocation_id":
				units[0].(map[string]any)[field] = strings.Repeat("9", 32)
			case "epoch":
				owner["epoch"] = "different-epoch"
			case "missing_unit":
				owner["units"] = units[:2]
			case "state":
				owner["state"] = "promoted"
			case "snapshot":
				owner["strixglm_snapshot"] = path + ".different"
			}
			if e := controllerWrite(c.Config.LegacyOwner, owner); e != nil {
				t.Fatal(e)
			}
			before, e := os.ReadFile(c.Config.LegacyOwner)
			if e != nil {
				t.Fatal(e)
			}
			actions := 0
			c.run = func(context.Context, int, ...string) ([]byte, error) {
				actions++
				return nil, errors.New("no command is permitted after foreign-owner rejection")
			}
			if e = c.RestoreLegacy(context.Background(), path); e == nil || !strings.Contains(e.Error(), "legacy") {
				t.Fatalf("foreign stopped owner accepted: %v", e)
			}
			after, e := os.ReadFile(c.Config.LegacyOwner)
			if e != nil || !bytes.Equal(before, after) {
				t.Fatal("foreign owner was overwritten")
			}
			if actions != 0 {
				t.Fatalf("performed %d commands despite foreign owner", actions)
			}
		})
	}
}

func TestRestoreLegacyAcceptsSameOwnerReorderedUnits(t *testing.T) {
	c, path, s, owner := stoppedLegacyRestoreFixture(t)
	units := owner["units"].([]any)
	units[0], units[2] = units[2], units[0]
	if e := controllerWrite(c.Config.LegacyOwner, owner); e != nil {
		t.Fatal(e)
	}
	if e := c.checkStoppedLegacyOwner(s, path); e != nil {
		t.Fatalf("same exact unit set rejected due to ordering: %v", e)
	}
}

func TestRestoreLegacyRereadsOwnerBeforeFirstMutation(t *testing.T) {
	c, path, s, owner := stoppedLegacyRestoreFixture(t)
	identities := map[string]map[string]string{}
	for rank := 0; rank < 2; rank++ {
		files := map[string]string{}
		for i := 0; i < 10; i++ {
			files[fmt.Sprintf("pinned/file-%02d", i)] = strings.Repeat("0", 64)
		}
		identities[fmt.Sprintf("rank%d", rank)] = files
	}
	var preserved map[string]any
	if e := json.Unmarshal(s.Owner, &preserved); e != nil {
		t.Fatal(e)
	}
	preserved["runtime_identity"] = identities
	var e error
	s.Owner, e = json.Marshal(preserved)
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(s.Owner)
	s.OwnerSHA256 = hex.EncodeToString(digest[:])
	if e = controllerWrite(path, s); e != nil {
		t.Fatal(e)
	}
	weights := []map[string]any{}
	for i := 0; i < 23; i++ {
		weights = append(weights, map[string]any{"rank": 0, "path": fmt.Sprintf("weights/shard-%02d", i), "size": 1})
	}
	if e = controllerWrite(c.Config.Manifest, map[string]any{"identity": identities, "weights": weights}); e != nil {
		t.Fatal(e)
	}
	hashCalls, mutations := 0, 0
	var changedOwner []byte
	c.run = func(ctx context.Context, rank int, args ...string) ([]byte, error) {
		switch args[0] {
		case "systemctl":
			if len(args) > 2 && args[2] == "show" {
				return []byte("ActiveState=inactive\nMainPID=0\n"), nil
			}
		case "stat":
			return []byte("1"), nil
		case "sha256sum":
			hashCalls++
			if hashCalls == 4 {
				// Simulate replacement during the final slow read-only identity
				// check, after the initial stopped-owner guard already passed.
				owner["units"].([]any)[1].(map[string]any)["invocation_id"] = strings.Repeat("9", 32)
				if e := controllerWrite(c.Config.LegacyOwner, owner); e != nil {
					return nil, e
				}
				changedOwner, e = os.ReadFile(c.Config.LegacyOwner)
				if e != nil {
					return nil, e
				}
			}
			var output strings.Builder
			for _, p := range args[2:] {
				fmt.Fprintf(&output, "%s  %s\n", strings.Repeat("0", 64), p)
			}
			return []byte(output.String()), nil
		}
		mutations++
		return nil, fmt.Errorf("unexpected mutation: %v", args)
	}
	if e = c.RestoreLegacy(context.Background(), path); e == nil || !strings.Contains(e.Error(), "identity changed") {
		t.Fatalf("owner replaced during read-only preflight accepted: %v", e)
	}
	if hashCalls != 4 || mutations != 0 {
		t.Fatalf("checks=%d mutations=%d", hashCalls, mutations)
	}
	after, e := os.ReadFile(c.Config.LegacyOwner)
	if e != nil || !bytes.Equal(after, changedOwner) {
		t.Fatal("replaced owner overwritten by first restore reservation")
	}
}
