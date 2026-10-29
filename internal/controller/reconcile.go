package controller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/domain"
)

type unfinishedOperation struct {
	opID string
	vmID string
	opType domain.OperationType
	state domain.OperationState
	workerID string
	endpoint string
}

// Reconcile queries workers for unfinished operations. It changes metadata only
// when a worker returns a concrete VM state; missing/unreachable workers leave
// the operation and reservation uncertain.
func (s *Service) Reconcile(ctx context.Context) error {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT o.id, o.vm_id, o.type, o.state, COALESCE(v.worker_id, ''), COALESCE(w.endpoint, '')
		FROM operations o JOIN vms v ON v.id=o.vm_id
		LEFT JOIN workers w ON w.id=v.worker_id
		WHERE o.state IN ('pending','dispatched','unknown')
		ORDER BY o.created_at, o.id`)
	if err != nil { return fmt.Errorf("load unfinished operations: %w", err) }
	var pending []unfinishedOperation
	for rows.Next() {
		var item unfinishedOperation
		var typ, state string
		if err := rows.Scan(&item.opID, &item.vmID, &typ, &state, &item.workerID, &item.endpoint); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan unfinished operation: %w", err)
		}
		item.opType, item.state = domain.OperationType(typ), domain.OperationState(state)
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil { _ = rows.Close(); return fmt.Errorf("iterate unfinished operations: %w", err) }
	if err := rows.Close(); err != nil { return fmt.Errorf("close unfinished operations: %w", err) }

	var failures []error
	for _, item := range pending {
		if item.workerID == "" || item.endpoint == "" {
			failures = append(failures, fmt.Errorf("operation %s has no assigned worker endpoint", item.opID))
			continue
		}
		report, err := s.workers.status(ctx, item.endpoint, item.vmID)
		if err != nil {
			// A timeout, 404 after worker restart, or unreachable host does not
			// prove the guest is stopped. Preserve unknown state and reservation.
			failures = append(failures, fmt.Errorf("query worker for operation %s: %w", item.opID, err))
			continue
		}
		observed, known := observedState(report.State)
		if !known {
			failures = append(failures, fmt.Errorf("worker returned unsupported state %q for VM %s", report.State, item.vmID))
			continue
		}
		opState, desired, finalObserved, reservation := reconcileDecision(item.opType, observed)
		if opState == domain.OperationUnknown {
			failures = append(failures, fmt.Errorf("worker state %s does not resolve operation %s", report.State, item.opID))
			continue
		}
		if err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
			now := s.now().UTC().Format(time.RFC3339Nano)
			if _, err := tx.ExecContext(ctx, "UPDATE operations SET state=?, error_code=NULL, error_message=NULL, updated_at=? WHERE id=?", string(opState), now, item.opID); err != nil {
				return fmt.Errorf("update reconciled operation: %w", err)
			}
			if _, err := tx.ExecContext(ctx, "UPDATE vms SET desired_state=?, observed_state=?, updated_at=? WHERE id=?", string(desired), string(finalObserved), now, item.vmID); err != nil {
				return fmt.Errorf("update reconciled VM: %w", err)
			}
			if reservation != "" {
				if _, err := tx.ExecContext(ctx, "UPDATE reservations SET state=?, updated_at=? WHERE vm_id=?", reservation, now, item.vmID); err != nil {
					return fmt.Errorf("update reconciled reservation: %w", err)
				}
			}
			return nil
		}); err != nil { failures = append(failures, fmt.Errorf("persist reconciliation for %s: %w", item.opID, err)) }
	}
	return errors.Join(failures...)
}

func observedState(state string) (domain.VMState, bool) {
	switch state {
	case "Created", "Shutdown":
		return domain.VMStopped, true
	case "Running":
		return domain.VMRunning, true
	default:
		return domain.VMUnknown, false
	}
}

func reconcileDecision(typ domain.OperationType, observed domain.VMState) (domain.OperationState, domain.VMState, domain.VMState, string) {
	switch typ {
	case domain.OperationPrepare:
		if observed == domain.VMStopped { return domain.OperationSucceeded, domain.VMStopped, domain.VMStopped, "active" }
	case domain.OperationStart:
		if observed == domain.VMRunning { return domain.OperationSucceeded, domain.VMRunning, domain.VMRunning, "active" }
		if observed == domain.VMStopped { return domain.OperationFailed, domain.VMStopped, domain.VMStopped, "active" }
	case domain.OperationStop:
		if observed == domain.VMStopped { return domain.OperationSucceeded, domain.VMStopped, domain.VMStopped, "active" }
		if observed == domain.VMRunning { return domain.OperationFailed, domain.VMRunning, domain.VMRunning, "active" }
	}
	return domain.OperationUnknown, "", domain.VMUnknown, ""
}

