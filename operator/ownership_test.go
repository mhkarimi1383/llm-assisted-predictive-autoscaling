package operator

import (
	"context"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(t *testing.T) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&coordinationv1.Lease{})
}

func TestOwnershipAcquireAndRenew(t *testing.T) {
	c := newFakeClient(t).Build()
	o := NewScalingOwnership(c, c, "operator-a")
	ctx := context.Background()

	ok, err := o.Acquire(ctx, "default", "web")
	if err != nil || !ok {
		t.Fatalf("first acquire must succeed: ok=%v err=%v", ok, err)
	}
	var lease coordinationv1.Lease
	if err := c.Get(ctx, client_leaseKey("default", "web"), &lease); err != nil {
		t.Fatalf("lease must exist: %v", err)
	}
	if *lease.Spec.HolderIdentity != "operator-a" {
		t.Fatalf("holder mismatch: %s", *lease.Spec.HolderIdentity)
	}

	// Re-acquire by the same holder renews.
	ok, err = o.Acquire(ctx, "default", "web")
	if err != nil || !ok {
		t.Fatalf("renew must succeed: ok=%v err=%v", ok, err)
	}
}

func TestOwnershipConflictAndTakeover(t *testing.T) {
	c := newFakeClient(t).Build()
	a := NewScalingOwnership(c, c, "operator-a")
	b := NewScalingOwnership(c, c, "operator-b")
	ctx := context.Background()

	if ok, _ := a.Acquire(ctx, "default", "web"); !ok {
		t.Fatal("a must acquire first")
	}
	ok, err := b.Acquire(ctx, "default", "web")
	if err != nil {
		t.Fatalf("conflict check error: %v", err)
	}
	if ok {
		t.Fatal("b must be denied while a's lease is live")
	}

	// Expire a's lease: b takes over (recovery protocol).
	var lease coordinationv1.Lease
	if err := c.Get(ctx, client_leaseKey("default", "web"), &lease); err != nil {
		t.Fatal(err)
	}
	stale := metav1.NewMicroTime(time.Now().Add(-2 * time.Minute))
	lease.Spec.RenewTime = &stale
	if err := c.Update(ctx, &lease); err != nil {
		t.Fatal(err)
	}
	ok, err = b.Acquire(ctx, "default", "web")
	if err != nil || !ok {
		t.Fatalf("b must take over the expired lease: ok=%v err=%v", ok, err)
	}
	if err := c.Get(ctx, client_leaseKey("default", "web"), &lease); err != nil {
		t.Fatal(err)
	}
	if *lease.Spec.HolderIdentity != "operator-b" {
		t.Fatalf("holder must be operator-b, got %s", *lease.Spec.HolderIdentity)
	}
}

func TestOwnershipRelease(t *testing.T) {
	c := newFakeClient(t).Build()
	o := NewScalingOwnership(c, c, "operator-a")
	ctx := context.Background()
	if ok, _ := o.Acquire(ctx, "default", "web"); !ok {
		t.Fatal("acquire failed")
	}
	if err := o.Release(ctx, "default", "web"); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	// Another operator can acquire immediately after release.
	b := NewScalingOwnership(c, c, "operator-b")
	if ok, _ := b.Acquire(ctx, "default", "web"); !ok {
		t.Fatal("b must acquire after a released")
	}
}

// helper: the lease key (kept out of the ownership file to avoid a
// client import there).
func client_leaseKey(ns, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: ns, Name: ownershipLeaseName(name)}
}
