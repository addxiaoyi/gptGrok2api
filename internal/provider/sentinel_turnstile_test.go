package provider

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestSolveSentinelTurnstileToken(t *testing.T) {
	key := "requirements-key"
	operations := [][]any{
		{float64(2), "token", "token-value"},
		{float64(3), "token-value"},
	}
	raw, err := json.Marshal(operations)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(xorSentinelString(string(raw), key)))
	token, err := solveSentinelTurnstileToken(encoded, key)
	if err != nil {
		t.Fatal(err)
	}
	if token != base64.StdEncoding.EncodeToString([]byte("token-value")) {
		t.Fatalf("unexpected token: %q", token)
	}
}

func TestDecodeSentinelDXPaddedEncoding(t *testing.T) {
	raw, err := decodeSentinelDX(base64.StdEncoding.EncodeToString([]byte("hello")), "key")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != xorSentinelString("hello", "key") {
		t.Fatalf("unexpected dx: %q", raw)
	}
}

func TestDecodeSentinelDXRawEncoding(t *testing.T) {
	raw, err := decodeSentinelDX(base64.RawStdEncoding.EncodeToString([]byte("hello")), "key")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != xorSentinelString("hello", "key") {
		t.Fatalf("unexpected dx: %q", raw)
	}
}

func TestDecodeSentinelDXEmptyKeyPassesThrough(t *testing.T) {
	raw, err := decodeSentinelDX(base64.StdEncoding.EncodeToString([]byte("plain")), "")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "plain" {
		t.Fatalf("expected passthrough, got %q", raw)
	}
}

func TestDecodeSentinelDXInvalidBase64(t *testing.T) {
	if _, err := decodeSentinelDX("!!!not-base64!!!", "key"); err == nil {
		t.Fatal("expected decode error")
	}
}

func newSentinelVMForTest() *sentinelVM {
	vm := &sentinelVM{values: map[any]any{}, result: ""}
	vm.install()
	return vm
}

func callSentinel(t *testing.T, vm *sentinelVM, opcode float64, args ...any) any {
	t.Helper()
	callable, ok := vm.get(opcode).(sentinelCallable)
	if !ok {
		t.Fatalf("opcode %v not installed", opcode)
	}
	return callable(args...)
}

func TestSentinelVMScalarOps(t *testing.T) {
	vm := newSentinelVMForTest()

	// opcode 1: XOR the value stored in the first slot with the value in the second slot
	callSentinel(t, vm, 2, "acc", "secret")
	callSentinel(t, vm, 2, "pad", "secret")
	callSentinel(t, vm, 1, "acc", "acc", "pad")
	if vm.get("acc") != xorSentinelString("secret", "secret") {
		t.Fatalf("xor op: %v", vm.get("acc"))
	}
	// too few args is a no-op
	callSentinel(t, vm, 1, "acc")

	callSentinel(t, vm, 2, "b", 42.0)
	if vm.get("b") != 42.0 {
		t.Fatalf("set op: %v", vm.get("b"))
	}
	callSentinel(t, vm, 3, "emit")
	if vm.result != base64.StdEncoding.EncodeToString([]byte("emit")) {
		t.Fatalf("emit op: %q", vm.result)
	}
	// emit with no args is a no-op
	callSentinel(t, vm, 3)

	// opcode 5 reads exactly args[0] and args[1] as slot references
	callSentinel(t, vm, 2, "s", "a")
	callSentinel(t, vm, 2, "letter", "b")
	callSentinel(t, vm, 5, "s", "letter")
	if vm.get("s") != "ab" {
		t.Fatalf("concat string: %v", vm.get("s"))
	}
	callSentinel(t, vm, 2, "n", 1.0)
	callSentinel(t, vm, 2, "two", 2.0)
	callSentinel(t, vm, 5, "n", "two")
	if vm.get("n") != "1.02.0" {
		t.Fatalf("concat number: %v", vm.get("n"))
	}
	callSentinel(t, vm, 2, "l", []any{"x"})
	callSentinel(t, vm, 2, "y", "y")
	callSentinel(t, vm, 5, "l", "y")
	if list := toAnyList(vm.get("l")); len(list) != 2 {
		t.Fatalf("concat list: %v", vm.get("l"))
	}
	// string branch: concatenates string with toString of incoming
	callSentinel(t, vm, 2, "d", "z")
	callSentinel(t, vm, 2, "seven", 7.0)
	callSentinel(t, vm, 5, "d", "seven")
	if vm.get("d") != "z7.0" {
		t.Fatalf("concat string+float: %v", vm.get("d"))
	}
	// too few args must be a no-op
	callSentinel(t, vm, 5, "ignored")
	if _, exists := vm.values["ignored"]; exists {
		t.Fatal("expected no write for short concat")
	}
}

func TestSentinelVMControlOps(t *testing.T) {
	vm := newSentinelVMForTest()

	// opcode 8: copy the raw value stored in the second slot into the first
	callSentinel(t, vm, 2, "b", 9.0)
	callSentinel(t, vm, 8, "alias", "b")
	if vm.get("alias") != 9.0 {
		t.Fatalf("alias op: %v", vm.get("alias"))
	}
	// too few args is a no-op
	callSentinel(t, vm, 8, "skipped")

	callSentinel(t, vm, 11, "b")
	if vm.get("b") != nil {
		t.Fatalf("delete op: %v", vm.get("b"))
	}

	callSentinel(t, vm, 2, "jsonText", "[1,2]")
	callSentinel(t, vm, 14, "parsed", "jsonText")
	if list := toAnyList(vm.get("parsed")); len(list) != 2 {
		t.Fatalf("json parse: %v", vm.get("parsed"))
	}
	// invalid JSON leaves the target untouched
	callSentinel(t, vm, 2, "keep", "orig")
	callSentinel(t, vm, 2, "badText", "not-json")
	callSentinel(t, vm, 14, "keep", "badText")
	if vm.get("keep") != "orig" {
		t.Fatalf("expected untouched target, got %v", vm.get("keep"))
	}

	callSentinel(t, vm, 2, "obj", 5.0)
	callSentinel(t, vm, 15, "json", "obj")
	if vm.get("json") != "5" {
		t.Fatalf("serialize: %v", vm.get("json"))
	}

	// opcodes 18/19 read and write vm.values[args[0]] directly
	callSentinel(t, vm, 19, "obj")
	if encoded := vm.get("obj"); encoded != base64.StdEncoding.EncodeToString([]byte("5.0")) {
		t.Fatalf("b64 encode: %v", encoded)
	}
	callSentinel(t, vm, 18, "obj")
	if vm.get("obj") != "5.0" {
		t.Fatalf("b64 decode: %v", vm.get("obj"))
	}
	// a value that is not valid base64 is left untouched
	callSentinel(t, vm, 2, "notB64", "!!!")
	callSentinel(t, vm, 18, "notB64")
	if vm.get("notB64") != "!!!" {
		t.Fatalf("expected untouched value, got %v", vm.get("notB64"))
	}

	// opcode 29 stores the strictly-less-than result of two slot values
	callSentinel(t, vm, 2, "cmp", 1.0)
	callSentinel(t, vm, 2, "two", 2.0)
	callSentinel(t, vm, 29, "lt", "cmp", "two")
	if vm.get("lt") != true {
		t.Fatalf("less-than true: %v", vm.get("lt"))
	}
	callSentinel(t, vm, 29, "lt2", "two", "cmp")
	if vm.get("lt2") != false {
		t.Fatalf("less-than false: %v", vm.get("lt2"))
	}
	// too few args is a no-op
	callSentinel(t, vm, 29, "noop", "cmp")

	callSentinel(t, vm, 2, "x", 6.0)
	callSentinel(t, vm, 33, "mul", "x", "two")
	if vm.get("mul") != 12.0 {
		t.Fatalf("multiply: %v", vm.get("mul"))
	}

	callSentinel(t, vm, 34, "copy", "x")
	if vm.get("copy") != 6.0 {
		t.Fatalf("copy: %v", vm.get("copy"))
	}

	// opcode 12 stores the whole values map
	callSentinel(t, vm, 12, "table")
	if _, ok := vm.get("table").(map[any]any); !ok {
		t.Fatalf("expected values map, got %T", vm.get("table"))
	}

	// opcode 25/26/28 are no-ops
	callSentinel(t, vm, 25, "a", "b")
	callSentinel(t, vm, 26, "a", "b")
	callSentinel(t, vm, 28, "a", "b")
}

func TestSentinelVMSubtractAndRemove(t *testing.T) {
	vm := newSentinelVMForTest()

	// opcode 27: args[0] is the destination and both operands are slot references
	callSentinel(t, vm, 2, "total", 10.0)
	callSentinel(t, vm, 2, "cost", 4.0)
	callSentinel(t, vm, 27, "total", "cost")
	if vm.get("total") != 6.0 {
		t.Fatalf("subtract: %v", vm.get("total"))
	}
	// too few args is a no-op
	callSentinel(t, vm, 27, "total")

	// list branch: remove the matching entry
	callSentinel(t, vm, 2, "list", []any{"a", "b", "c"})
	callSentinel(t, vm, 2, "needle", "b")
	callSentinel(t, vm, 27, "list", "needle")
	if list := toAnyList(vm.get("list")); len(list) != 2 {
		t.Fatalf("remove: %v", vm.get("list"))
	}
	// a missing entry leaves the list untouched
	callSentinel(t, vm, 2, "absent", "zzz")
	callSentinel(t, vm, 27, "list", "absent")
	if list := toAnyList(vm.get("list")); len(list) != 2 {
		t.Fatalf("remove missing: %v", vm.get("list"))
	}
}

func TestSentinelVMPropertyAndCallTargets(t *testing.T) {
	vm := newSentinelVMForTest()

	// opcode 6/24 read the property from the slot in args[1] using the slot in args[2]
	callSentinel(t, vm, 2, "doc", "window.document")
	callSentinel(t, vm, 2, "locationKey", "location")
	callSentinel(t, vm, 6, "loc", "doc", "locationKey")
	if vm.get("loc") != "https://chatgpt.com/" {
		t.Fatalf("window.location: %v", vm.get("loc"))
	}
	callSentinel(t, vm, 2, "obj", map[string]any{"a": 1.0})
	callSentinel(t, vm, 2, "aKey", "a")
	callSentinel(t, vm, 24, "prop", "obj", "aKey")
	if vm.get("prop") != 1.0 {
		t.Fatalf("map property: %v", vm.get("prop"))
	}

	created, _ := vm.callTargetChecked("window.Object.create", nil)
	ordered, ok := created.(*sentinelOrderedMap)
	if !ok {
		t.Fatalf("expected ordered map, got %T", created)
	}
	// keys are empty until Reflect.set records an insertion order
	keys, err := vm.callTargetChecked("window.Object.keys", []any{ordered})
	if err != nil {
		t.Fatal(err)
	}
	if len(toAnyList(keys)) != 0 {
		t.Fatalf("expected empty key list, got %v", keys)
	}
	if _, err := vm.callTargetChecked("window.Reflect.set", []any{ordered, "k", "v"}); err != nil {
		t.Fatal(err)
	}
	if ordered.keys[0] != "k" || ordered.values["k"] != "v" {
		t.Fatalf("reflect set on ordered map: %#v", ordered)
	}
	// re-setting an existing key must not duplicate the insertion order
	if _, err := vm.callTargetChecked("window.Reflect.set", []any{ordered, "k", "v2"}); err != nil {
		t.Fatal(err)
	}
	if len(ordered.keys) != 1 {
		t.Fatalf("expected one key, got %v", ordered.keys)
	}
	plain := map[string]any{}
	if _, err := vm.callTargetChecked("window.Reflect.set", []any{plain, "k", "v"}); err != nil {
		t.Fatal(err)
	}
	if plain["k"] != "v" {
		t.Fatalf("reflect set on plain map: %#v", plain)
	}
	// two-arg Reflect.set falls through to the false return
	if out, _ := vm.callTargetChecked("window.Reflect.set", []any{plain, "k"}); out != false {
		t.Fatalf("expected false, got %v", out)
	}
	// localStorage key list
	storageKeys, _ := vm.callTargetChecked("window.Object.keys", []any{"window.localStorage"})
	if len(toAnyList(storageKeys)) == 0 {
		t.Fatal("expected localStorage key list")
	}
	mapKeys, _ := vm.callTargetChecked("window.Object.keys", []any{map[string]any{"a": 1.0}})
	if len(toAnyList(mapKeys)) != 1 {
		t.Fatalf("expected map keys, got %v", mapKeys)
	}
	// Object.keys on a non-list, non-string target returns nil
	if out, _ := vm.callTargetChecked("window.Object.keys", []any{42.0}); out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
	if elapsed, _ := vm.callTargetChecked("window.performance.now", nil); elapsed == nil {
		t.Fatal("expected performance value")
	}
	if out, _ := vm.callTargetChecked("unknown.target", nil); out != nil {
		t.Fatalf("expected nil for unknown target, got %v", out)
	}
	// a non-callable, non-string target also yields nil
	if out, _ := vm.callTargetChecked(42.0, nil); out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
}

func TestSentinelVMTryAndBranchOps(t *testing.T) {
	vm := newSentinelVMForTest()

	// opcode 13 resolves its target from a slot; a known target stores no error
	callSentinel(t, vm, 2, "target", "window.performance.now")
	callSentinel(t, vm, 13, "out", "target")
	if _, exists := vm.values["out"]; exists {
		t.Fatalf("expected no error value, got %v", vm.get("out"))
	}
	// a single argument is below the op's guard and does nothing
	callSentinel(t, vm, 13, "short")

	// opcode 20: call the target in args[2] only when both operands are equal
	vm.values["hit"] = false
	callSentinel(t, vm, 2, "callback", sentinelCallable(func(...any) any {
		vm.values["hit"] = true
		return nil
	}))
	callSentinel(t, vm, 2, "a", 1.0)
	callSentinel(t, vm, 2, "b", 1.0)
	callSentinel(t, vm, 2, "c", 9.0)
	callSentinel(t, vm, 20, "a", "b", "callback")
	if vm.get("hit") != true {
		t.Fatal("expected equal operands to call the target")
	}
	vm.values["hit"] = false
	callSentinel(t, vm, 20, "a", "c", "callback")
	if vm.get("hit") != false {
		t.Fatal("expected unequal operands to skip the target")
	}
	// too few args is a no-op
	callSentinel(t, vm, 20, "a", "b")

	// opcode 21: drift guard fires only when elapsed exceeds the threshold
	callSentinel(t, vm, 2, "start", 100.0)
	callSentinel(t, vm, 2, "last", 0.0)
	callSentinel(t, vm, 2, "budget", 1.0)
	callSentinel(t, vm, 21, "start", "last", "budget", "callback")
	if vm.get("hit") != true {
		t.Fatal("expected drift guard to call the target")
	}
	vm.values["hit"] = false
	callSentinel(t, vm, 21, "start", "start", "budget", "callback")
	if vm.get("hit") != false {
		t.Fatal("expected drift guard to stay quiet within the budget")
	}
	// too few args is a no-op
	callSentinel(t, vm, 21, "start", "last")
}

func TestSentinelVMDeferredAndCapturedQueues(t *testing.T) {
	// opcode 30 with a captured queue in args[3] and capture keys in args[2]
	vm := newSentinelVMForTest()
	captured := []any{[]any{float64(2), "captured", "value"}}
	callSentinel(t, vm, 30, "fn", float64(0), []any{"slot"}, captured)
	fn, ok := vm.get("fn").(sentinelCallable)
	if !ok {
		t.Fatalf("expected captured callable, got %T", vm.get("fn"))
	}
	fn("value")
	if vm.get("slot") != "value" || vm.get("captured") != "value" {
		t.Fatalf("captured queue did not run: %v %v", vm.get("slot"), vm.get("captured"))
	}

	// fewer call args than capture keys only fills the leading slots
	callSentinel(t, vm, 30, "fn3", float64(0), []any{"first", "second"}, captured)
	fn3 := vm.get("fn3").(sentinelCallable)
	fn3("only-one")
	if vm.get("first") != "only-one" {
		t.Fatalf("first capture slot: %v", vm.get("first"))
	}
	if _, exists := vm.values["second"]; exists {
		t.Fatalf("expected second slot untouched, got %v", vm.get("second"))
	}

	// without a []any tail the queue is read from args[2] instead
	vm2 := newSentinelVMForTest()
	callSentinel(t, vm2, 30, "fn2", float64(0), []any{[]any{float64(2), "plain", "set"}}, "not-a-list")
	fn2, ok := vm2.get("fn2").(sentinelCallable)
	if !ok {
		t.Fatalf("expected callable, got %T", vm2.get("fn2"))
	}
	fn2()
	if vm2.get("plain") != "set" {
		t.Fatalf("plain queue did not run: %v", vm2.get("plain"))
	}
}

func TestSentinelVMDeferredCallAndInvoke(t *testing.T) {
	vm := newSentinelVMForTest()

	// opcode 7: call the target in args[0] with the evaluated args in args[1:]
	vm.values["sum"] = sentinelCallable(func(args ...any) any {
		if len(args) < 2 {
			return nil
		}
		return numberSentinel(args[0]) + numberSentinel(args[1])
	})
	callSentinel(t, vm, 2, "a", 1.0)
	callSentinel(t, vm, 2, "b", 2.0)
	callSentinel(t, vm, 7, "sum", "a", "b")
	if vm.get("a") != 1.0 || vm.get("b") != 2.0 {
		t.Fatalf("call op should not overwrite the operands: %v %v", vm.get("a"), vm.get("b"))
	}
	// an unknown target is simply dropped
	callSentinel(t, vm, 7, "missing", "a")
	// a single argument is still forwarded to the callee
	callSentinel(t, vm, 7, "sum", "a")
	// with no arguments at all the op is a no-op
	callSentinel(t, vm, 7)

	// opcode 17: store whatever the target returns
	callSentinel(t, vm, 2, "target", "window.performance.now")
	callSentinel(t, vm, 17, "elapsed", "target")
	if _, ok := vm.get("elapsed").(float64); !ok {
		t.Fatalf("stored callable result: %v", vm.get("elapsed"))
	}
	// too few args is a no-op
	callSentinel(t, vm, 17, "skipped", "target")

	// opcode 22: run a nested queue, then restore the previous one
	callSentinel(t, vm, 2, "inner", "from-nested")
	callSentinel(t, vm, 22, "res", []any{[]any{float64(2), "inner", "from-nested"}})
	if vm.get("res") != "None" {
		t.Fatalf("expected None marker, got %v", vm.get("res"))
	}
	if vm.get("inner") != "from-nested" {
		t.Fatalf("nested queue did not apply: %v", vm.get("inner"))
	}
	// too few args is a no-op
	callSentinel(t, vm, 22, "res")

	// opcode 23: invoke a stored callable, keyed by its numeric slot
	callSentinel(t, vm, 2, "tag", "seed")
	callSentinel(t, vm, 23, "tag", float64(26))
	// a nil receiver and a non-callable callee are both ignored
	callSentinel(t, vm, 23, "missing", float64(26))
	callSentinel(t, vm, 23, "tag", "not-a-callable")
	// too few args is a no-op
	callSentinel(t, vm, 23, "tag")
}

func TestSentinelVMRunQueueEdges(t *testing.T) {
	vm := newSentinelVMForTest()

	// an empty queue returns immediately
	if err := vm.runQueue(10); err != nil {
		t.Fatal(err)
	}

	// empty tokens are skipped and unknown opcodes drain without effect
	vm.values[float64(9)] = []any{
		[]any{},
		[]any{"not-an-opcode"},
		[]any{float64(2), "reached", true},
	}
	if err := vm.runQueue(10); err != nil {
		t.Fatal(err)
	}
	if vm.get("reached") != true {
		t.Fatalf("queue did not drain: %v", vm.get("reached"))
	}

	// a queue token that puts itself back never drains and trips the step limit
	looping := newSentinelVMForTest()
	var selfRestoring []any
	looping.values["requeue"] = sentinelCallable(func(...any) any {
		looping.values[float64(9)] = selfRestoring
		return nil
	})
	selfRestoring = []any{[]any{"requeue"}}
	looping.values[float64(9)] = selfRestoring
	if err := looping.runQueue(100); err == nil {
		t.Fatal("expected step limit error")
	}
}

func TestSentinelVMPanicIsRecovered(t *testing.T) {
	vm := newSentinelVMForTest()
	// opcode 3 calls base64 encode; feeding non-string args via runQueue can cause
	// a panic in toString which is recovered by the VM
	vm.values[float64(9)] = []any{
		[]any{float64(3), float64(0)}, // panic: base64 on non-string
		[]any{float64(2), "survivor", true},
	}
	if err := vm.runQueue(50); err != nil {
		t.Fatal(err)
	}
	if vm.get("survivor") != true {
		t.Fatal("expected the queue to continue after a panic")
	}
}
