// SPDX-License-Identifier: BSD-3-Clause

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"

	nbv1alpha1 "github.com/netbirdio/kubernetes-operator/api/v1alpha1"
	"github.com/netbirdio/kubernetes-operator/internal/netbirdmock"
)

var _ = Describe("TCPRoute Controller", func() {
	Context("When reconciling a TCPRoute", func() {
		ctx := context.Background()

		var (
			tcpRouteRec     *TCPRouteReconciler
			netResourceRec  *NetworkResourceReconciler
			netRouterRec    *NetworkRouterReconciler
			setupKeyRec     *SetupKeyReconciler
			groupRec        *GroupReconciler
			nbClient        *netbird.Client
			ns, gwClassName string
		)

		BeforeEach(func() {
			nbClient = netbirdmock.Client()
			tcpRouteRec = &TCPRouteReconciler{Client: k8sClient}
			netResourceRec = &NetworkResourceReconciler{Client: k8sClient, Netbird: nbClient}
			netRouterRec = &NetworkRouterReconciler{
				Client:        k8sClient,
				Netbird:       nbClient,
				ClientImage:   "docker.io/netbirdio/netbird:latest",
				ManagementURL: "https://netbird.io",
			}
			setupKeyRec = &SetupKeyReconciler{Client: k8sClient, Netbird: nbClient}
			groupRec = &GroupReconciler{Client: k8sClient, Netbird: nbClient}

			nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "tcproute-"}}
			Expect(k8sClient.Create(ctx, nsObj)).To(Succeed())
			ns = nsObj.Name

			gwc := &gwv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "netbird-"},
				Spec:       gwv1.GatewayClassSpec{ControllerName: gwv1.GatewayController(GatewayControllerName)},
			}
			Expect(k8sClient.Create(ctx, gwc)).To(Succeed())
			gwClassName = gwc.Name
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, &gwv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: gwClassName}})
			nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(nsObj), nsObj); kerrors.IsNotFound(err) {
				return
			}
			Expect(k8sClient.Delete(ctx, nsObj)).To(Succeed())
		})

		It("creates a NetworkResource that becomes a host resource per family", func() {
			_, err := nbClient.DNSZones.CreateZone(ctx, api.ZoneRequest{Name: "cluster.local", Domain: "cluster.local"})
			Expect(err).ToNot(HaveOccurred())

			netRouter := &nbv1alpha1.NetworkRouter{
				ObjectMeta: metav1.ObjectMeta{Name: "kube", Namespace: ns},
				Spec:       nbv1alpha1.NetworkRouterSpec{DNSZoneRef: nbv1alpha1.DNSZoneReference{Name: "cluster.local"}},
			}
			Expect(k8sClient.Create(ctx, netRouter)).To(Succeed())
			routerNN := client.ObjectKey{Name: "kube", Namespace: ns}
			for range 3 {
				_, err := netRouterRec.Reconcile(ctx, reconcile.Request{NamespacedName: routerNN})
				Expect(err).NotTo(HaveOccurred())
				key := client.ObjectKey{Name: "networkrouter-kube", Namespace: ns}
				_, err = groupRec.Reconcile(ctx, reconcile.Request{NamespacedName: key})
				Expect(err).NotTo(HaveOccurred())
				_, err = setupKeyRec.Reconcile(ctx, reconcile.Request{NamespacedName: key})
				Expect(err).NotTo(HaveOccurred())
			}

			gw := &gwv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "private", Namespace: ns},
				Spec: gwv1.GatewaySpec{
					GatewayClassName: gwv1.ObjectName(gwClassName),
					Listeners: []gwv1.Listener{{
						Name:     gwv1.SectionName("kube"),
						Protocol: gwv1.ProtocolType("gateway.netbird.io/NetworkRouter"),
						Port:     gwv1.PortNumber(1),
					}},
				},
			}
			Expect(k8sClient.Create(ctx, gw)).To(Succeed())
			meta.SetStatusCondition(&gw.Status.Conditions, metav1.Condition{
				Type:               string(gwv1.GatewayConditionProgrammed),
				Status:             metav1.ConditionTrue,
				Reason:             string(gwv1.GatewayReasonProgrammed),
				ObservedGeneration: gw.Generation,
			})
			Expect(k8sClient.Status().Update(ctx, gw)).To(Succeed())

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: ns},
				Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 5432}}},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(svc), svc)).To(Succeed())

			port := gwv1.PortNumber(5432)
			tr := &gwv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: ns},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: gwv1.ObjectName(gw.Name)}}},
					Rules: []gwv1alpha2.TCPRouteRule{{
						BackendRefs: []gwv1.BackendRef{{
							BackendObjectReference: gwv1.BackendObjectReference{Name: "db", Port: &port},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			_, err = tcpRouteRec.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Name: "db", Namespace: ns}})
			Expect(err).NotTo(HaveOccurred())

			// The route applied a NetworkResource for the backend Service.
			netResource := &nbv1alpha1.NetworkResource{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: ns}}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(netResource), netResource)).To(Succeed())
			Expect(netResource.Spec.ServiceRef.Name).To(Equal("db"))
			Expect(netResource.Spec.NetworkRouterRef.Name).To(Equal("kube"))

			// The NetworkResource controller turns it into a host resource at the
			// ClusterIP (no domain/routing-mode involved).
			_, err = netResourceRec.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Name: "db", Namespace: ns}})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(netResource), netResource)).To(Succeed())
			Expect(netResource.Status.Resources).NotTo(BeEmpty())
			Expect(netResource.Status.Resources[0].Address).To(Equal(svc.Spec.ClusterIP))
			Expect(netResource.Status.Resources[0].ResourceID).NotTo(BeEmpty())
		})
	})
})
