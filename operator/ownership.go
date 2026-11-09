// Scaling ownership lock (Algorithm 1).
//
// Exclusive per-workload scaling ownership is held as a Lease
// (coordination.k8s.io/v1) named after the target Deployment. The
// lease serializes decision cycles against overlapping reconciles
// (the duplicate-execution defect observed in v1 testing) and
// survives operator restarts: a lease left by a dead operator
// expires after its duration, and any surviving operator takes it
// over through the documented recovery protocol.
package operator

import (
	"context"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const leaseNamePrefix = "llm-autoscaler-own-"

// ScalingOwnership acquires and renews per-workload leases.
// Reads go through a non-caching reader so no Lease informer (and
// no cluster-wide list/watch permission) is needed; writes are
// always direct API calls anyway.
type ScalingOwnership struct {
	reader        client.Reader
	writer        client.Writer
	holder        string
	leaseDuration time.Duration
}

// NewScalingOwnership builds the ownership manager. The holder
// identity should be unique per operator instance (pod name). Pass
// the manager's APIReader as the reader.
func NewScalingOwnership(reader client.Reader, writer client.Writer, holder string) *ScalingOwnership {
	return &ScalingOwnership{reader: reader, writer: writer, holder: holder, leaseDuration: 90 * time.Second}
}

func ownershipLeaseName(deployment string) string {
	return leaseNamePrefix + deployment
}

// Acquire obtains or renews exclusive scaling ownership for the
// workload. It returns false (without error) when another live
// operator holds the lease.
func (o *ScalingOwnership) Acquire(ctx context.Context, namespace, deployment string) (bool, error) {
	key := types.NamespacedName{Namespace: namespace, Name: ownershipLeaseName(deployment)}
	now := metav1.NewMicroTime(time.Now())

	var lease coordinationv1.Lease
	err := o.reader.Get(ctx, key, &lease)
	if apierrors.IsNotFound(err) {
		lease = coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      ownershipLeaseName(deployment),
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "llm-autoscaler"},
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:        &o.holder,
				LeaseDurationSeconds:  ptr.To(int32(o.leaseDuration.Seconds())),
				AcquireTime:           &now,
				RenewTime:             &now,
				LeaseTransitions:      ptr.To(int32(0)),
			},
		}
		if createErr := o.writer.Create(ctx, &lease); createErr != nil {
			if apierrors.IsAlreadyExists(createErr) {
				return false, nil // raced with another acquirer
			}
			return false, createErr
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}

	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == o.holder {
		// Ours (or orphaned): renew.
		lease.Spec.HolderIdentity = &o.holder
		lease.Spec.LeaseDurationSeconds = ptr.To(int32(o.leaseDuration.Seconds()))
		lease.Spec.RenewTime = &now
		if lease.Spec.AcquireTime == nil {
			lease.Spec.AcquireTime = &now
		}
		return true, o.writer.Update(ctx, &lease)
	}

	// Held by another operator: expired leases are taken over
	// (Algorithm 1 recovery protocol).
	if lease.Spec.RenewTime != nil &&
		now.Sub(lease.Spec.RenewTime.Time) > o.leaseDuration {
		lease.Spec.HolderIdentity = &o.holder
		lease.Spec.AcquireTime = &now
		lease.Spec.RenewTime = &now
		lease.Spec.LeaseTransitions = ptr.To(ptr.Deref(lease.Spec.LeaseTransitions, 0) + 1)
		return true, o.writer.Update(ctx, &lease)
	}
	return false, nil
}

// Release gives up ownership (used when the cycle ends with the
// workload replaced or the operator shutting down).
func (o *ScalingOwnership) Release(ctx context.Context, namespace, deployment string) error {
	key := types.NamespacedName{Namespace: namespace, Name: ownershipLeaseName(deployment)}
	var lease coordinationv1.Lease
	if err := o.reader.Get(ctx, key, &lease); err != nil {
		return client.IgnoreNotFound(err)
	}
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == o.holder {
		lease.Spec.HolderIdentity = nil
		return o.writer.Update(ctx, &lease)
	}
	return nil
}
