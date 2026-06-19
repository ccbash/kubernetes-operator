// SPDX-License-Identifier: BSD-3-Clause

package controller

import (
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// logConstructor builds the per-controller logger used for both the framework's
// own messages (Starting Controller, Reconciler error, …) and the reconcilers'
// logs via LoggerFrom(ctx). It replaces controller-runtime's default, which
// repeats the kind and the object's namespace/name several times per line
// (controller, controllerGroup, controllerKind, <Kind>={…}, namespace, name).
//
// Instead it names the logger after the kind and, per reconcile, adds just the
// object's namespace and name. controller-runtime still appends a reconcileID so
// a single reconcile's lines can be correlated.
func logConstructor(mgr ctrl.Manager, kind string) func(*reconcile.Request) logr.Logger {
	base := mgr.GetLogger().WithName(kind)
	return func(req *reconcile.Request) logr.Logger {
		if req == nil {
			return base
		}
		return base.WithValues("namespace", req.Namespace, "name", req.Name)
	}
}
