// SPDX-License-Identifier: BSD-3-Clause

package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"

	nbv1alpha1 "github.com/netbirdio/kubernetes-operator/api/v1alpha1"
	"github.com/netbirdio/kubernetes-operator/internal/netbirdmock"
)

var _ = Describe("v0.11 mirror + translation", func() {
	ctx := context.Background()

	var (
		nbClient *netbird.Client
		controls *netbirdmock.Controls
		ns       string
	)

	BeforeEach(func() {
		nbClient, controls = netbirdmock.ClientWithControls()
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "redesign-"}}
		Expect(k8sClient.Create(ctx, nsObj)).To(Succeed())
		ns = nsObj.Name
	})

	reconcileOnce := func(r interface {
		Reconcile(context.Context, reconcile.Request) (reconcile.Result, error)
	}, name string) (reconcile.Result, error) {
		return r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Name: name, Namespace: ns}})
	}

	// readyNetwork creates a Network and reconciles it to Ready.
	readyNetwork := func(name string) *nbv1alpha1.Network {
		network := &nbv1alpha1.Network{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec:       nbv1alpha1.NetworkSpec{Name: name},
		}
		Expect(k8sClient.Create(ctx, network)).To(Succeed())
		_, err := reconcileOnce(NewNetworkReconciler(k8sClient, nbClient, nil), name)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(network), network)).To(Succeed())
		Expect(network.Status.NetworkID).NotTo(BeEmpty())
		return network
	}

	Describe("Layer-1 mirror reconcilers", func() {
		It("Network creates the NetBird network and goes Ready", func() {
			network := readyNetwork("kube")
			Expect(meta.IsStatusConditionTrue(network.Status.Conditions, nbv1alpha1.ReadyCondition)).To(BeTrue())
		})

		It("DNSZone creates the zone and goes Ready", func() {
			zone := &nbv1alpha1.DNSZone{
				ObjectMeta: metav1.ObjectMeta{Name: "zone", Namespace: ns},
				Spec:       nbv1alpha1.DNSZoneSpec{Name: "kube.example.com", Domain: "kube.example.com", Enabled: true},
			}
			Expect(k8sClient.Create(ctx, zone)).To(Succeed())
			_, err := reconcileOnce(NewDNSZoneReconciler(k8sClient, nbClient, nil), "zone")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(zone), zone)).To(Succeed())
			Expect(zone.Status.ZoneID).NotTo(BeEmpty())
		})

		It("NetworkResource requeues until its Network is ready, then creates the resource", func() {
			nr := &nbv1alpha1.NetworkResource{
				ObjectMeta: metav1.ObjectMeta{Name: "res", Namespace: ns},
				Spec: nbv1alpha1.NetworkResourceSpec{
					NetworkRef: nbv1alpha1.CrossNamespaceReference{Name: "kube", Namespace: ns},
					Name:       "res",
					Address:    "10.0.0.5",
					Enabled:    true,
				},
			}
			Expect(k8sClient.Create(ctx, nr)).To(Succeed())
			r := NewNetworkResourceReconciler(k8sClient, nbClient, nil)

			// Network missing -> dependency not ready: requeue, no error, not Ready.
			res, err := reconcileOnce(r, "res")
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0))
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nr), nr)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(nr.Status.Conditions, nbv1alpha1.ReadyCondition)).To(BeFalse())

			readyNetwork("kube")
			_, err = reconcileOnce(r, "res")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nr), nr)).To(Succeed())
			Expect(nr.Status.ResourceID).NotTo(BeEmpty())
			Expect(nr.Status.NetworkID).NotTo(BeEmpty())
			Expect(meta.IsStatusConditionTrue(nr.Status.Conditions, nbv1alpha1.ReadyCondition)).To(BeTrue())
		})

		It("DNSRecord resolves its zone and creates the record", func() {
			zone := &nbv1alpha1.DNSZone{
				ObjectMeta: metav1.ObjectMeta{Name: "zone", Namespace: ns},
				Spec:       nbv1alpha1.DNSZoneSpec{Name: "kube.example.com", Domain: "kube.example.com", Enabled: true},
			}
			Expect(k8sClient.Create(ctx, zone)).To(Succeed())
			_, err := reconcileOnce(NewDNSZoneReconciler(k8sClient, nbClient, nil), "zone")
			Expect(err).NotTo(HaveOccurred())

			rec := &nbv1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{Name: "rec", Namespace: ns},
				Spec: nbv1alpha1.DNSRecordSpec{
					ZoneRef: nbv1alpha1.CrossNamespaceReference{Name: "zone", Namespace: ns},
					Name:    "app.kube.example.com",
					Type:    "A",
					Content: "10.0.0.5",
					TTL:     300,
				},
			}
			Expect(k8sClient.Create(ctx, rec)).To(Succeed())
			_, err = reconcileOnce(NewDNSRecordReconciler(k8sClient, nbClient, nil), "rec")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rec), rec)).To(Succeed())
			Expect(rec.Status.RecordID).NotTo(BeEmpty())
		})
	})

	Describe("Route translation", func() {
		// programmedGateway creates a GatewayClass + a programmed Gateway linked to
		// the named Network and hostname, plus the Network and DNSZone CRDs the
		// route controllers resolve.
		programmedGateway := func(networkName, hostname string) *gwv1.Gateway {
			gwc := &gwv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "netbird-"},
				Spec:       gwv1.GatewayClassSpec{ControllerName: gwv1.GatewayController(GatewayControllerName)},
			}
			Expect(k8sClient.Create(ctx, gwc)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, &gwv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: gwc.Name}})
			})

			Expect(k8sClient.Create(ctx, &nbv1alpha1.Network{
				ObjectMeta: metav1.ObjectMeta{Name: networkName, Namespace: ns},
				Spec:       nbv1alpha1.NetworkSpec{Name: networkName},
			})).To(Succeed())

			gw := &gwv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: ns},
				Spec: gwv1.GatewaySpec{
					GatewayClassName: gwv1.ObjectName(gwc.Name),
					Listeners: []gwv1.Listener{{
						Name:     gwv1.SectionName(networkName),
						Protocol: gwv1.ProtocolType("gateway.netbird.io/Network"),
						Port:     gwv1.PortNumber(1),
						Hostname: (*gwv1.Hostname)(&hostname),
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

			Expect(k8sClient.Create(ctx, &nbv1alpha1.DNSZone{
				ObjectMeta: metav1.ObjectMeta{Name: gw.Name, Namespace: ns},
				Spec:       nbv1alpha1.DNSZoneSpec{Name: hostname, Domain: hostname, Enabled: true},
			})).To(Succeed())
			return gw
		}

		makeService := func(name string) *corev1.Service {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(svc), svc)).To(Succeed())
			Expect(svc.Spec.ClusterIP).NotTo(BeEmpty())
			return svc
		}

		makeHTTPRoute := func(gw *gwv1.Gateway, svcName, hostname string) *gwv1.HTTPRoute {
			port := gwv1.PortNumber(80)
			hr := &gwv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: ns},
				Spec: gwv1.HTTPRouteSpec{
					CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: gwv1.ObjectName(gw.Name)}}},
					Hostnames:       []gwv1.Hostname{gwv1.Hostname(hostname)},
					Rules: []gwv1.HTTPRouteRule{{
						BackendRefs: []gwv1.HTTPBackendRef{{BackendRef: gwv1.BackendRef{
							BackendObjectReference: gwv1.BackendObjectReference{Name: gwv1.ObjectName(svcName), Port: &port},
						}}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, hr)).To(Succeed())
			return hr
		}

		It("HTTPRoute creates a NetworkResource and DNSRecord per backend", func() {
			gw := programmedGateway("kube", "kube.example.com")
			svc := makeService("app")
			makeHTTPRoute(gw, "app", "app.example.com")

			r := &HTTPRouteReconciler{Client: k8sClient}
			_, err := reconcileOnce(r, "app")
			Expect(err).NotTo(HaveOccurred())

			nr := &nbv1alpha1.NetworkResource{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "app-ipv4", Namespace: ns}, nr)).To(Succeed())
			Expect(nr.Spec.Address).To(Equal(svc.Spec.ClusterIP))
			Expect(nr.Spec.NetworkRef.Name).To(Equal("kube"))

			rec := &nbv1alpha1.DNSRecord{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "app-ipv4", Namespace: ns}, rec)).To(Succeed())
			Expect(rec.Spec.Name).To(Equal(fmt.Sprintf("app-%s.kube.example.com", ns)))
			Expect(rec.Spec.Type).To(Equal("A"))
			Expect(rec.Spec.Content).To(Equal(svc.Spec.ClusterIP))
			Expect(rec.Spec.ZoneRef.Name).To(Equal(gw.Name))
		})

		It("ReverseProxyService builds a cluster target from the route", func() {
			gw := programmedGateway("kube", "kube.example.com")
			makeService("app")
			makeHTTPRoute(gw, "app", "app.example.com")
			controls.AddProxyCluster("cluster-1", "gate.example.com")

			rps := &nbv1alpha1.ReverseProxyService{
				ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: ns},
				Spec: nbv1alpha1.ReverseProxyServiceSpec{
					RouteRef:     nbv1alpha1.RouteReference{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "app"},
					ProxyCluster: "gate.example.com",
					Upstream:     nbv1alpha1.UpstreamModeHostname,
				},
			}
			Expect(k8sClient.Create(ctx, rps)).To(Succeed())

			_, err := reconcileOnce(NewReverseProxyServiceReconciler(k8sClient, nbClient, nil), "app")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(rps), rps)).To(Succeed())
			Expect(rps.Status.ServiceID).NotTo(BeEmpty())

			services, err := nbClient.ReverseProxyServices.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(services).To(HaveLen(1))
			Expect(services[0].Domain).To(Equal("app.example.com"))
			Expect(services[0].Targets).To(HaveLen(1))
			target := services[0].Targets[0]
			Expect(target.TargetType).To(Equal(api.ServiceTargetTargetTypeCluster))
			Expect(target.TargetId).To(Equal("cluster-1"))
			Expect(target.Host).NotTo(BeNil())
			Expect(*target.Host).To(Equal(fmt.Sprintf("app-%s.kube.example.com", ns)))
			Expect(target.Options).NotTo(BeNil())
			Expect(target.Options.DirectUpstream).NotTo(BeNil())
			Expect(*target.Options.DirectUpstream).To(BeTrue())
		})
	})

	Describe("Gateway orchestrator", func() {
		It("creates the router children, joins the network, and programs", func() {
			network := readyNetwork("kube")

			gwc := &gwv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "netbird-"},
				Spec:       gwv1.GatewayClassSpec{ControllerName: gwv1.GatewayController(GatewayControllerName)},
			}
			Expect(k8sClient.Create(ctx, gwc)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, &gwv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: gwc.Name}})
			})

			host := "kube.example.com"
			gw := &gwv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: ns},
				Spec: gwv1.GatewaySpec{
					GatewayClassName: gwv1.ObjectName(gwc.Name),
					Listeners: []gwv1.Listener{{
						Name:     gwv1.SectionName("kube"),
						Protocol: gwv1.ProtocolType("gateway.netbird.io/Network"),
						Port:     gwv1.PortNumber(1),
						Hostname: (*gwv1.Hostname)(&host),
					}},
				},
			}
			Expect(k8sClient.Create(ctx, gw)).To(Succeed())

			gwRec := &GatewayReconciler{Client: k8sClient, Netbird: nbClient, ManagementURL: "https://netbird.io", ClientImage: "netbird:latest"}

			// First pass: creates Group/DNSZone/SetupKey, waits on the setup key.
			_, err := reconcileOnce(gwRec, "gw")
			Expect(err).NotTo(HaveOccurred())

			zone := &nbv1alpha1.DNSZone{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "gw", Namespace: ns}, zone)).To(Succeed())
			Expect(zone.Spec.Domain).To(Equal(host))

			// Reconcile the owned Group + SetupKey so their ids/secret exist.
			_, err = reconcileOnce(&GroupReconciler{Client: k8sClient, Netbird: nbClient}, "gw-router")
			Expect(err).NotTo(HaveOccurred())
			_, err = reconcileOnce(&SetupKeyReconciler{Client: k8sClient, Netbird: nbClient}, "gw-router")
			Expect(err).NotTo(HaveOccurred())

			// Second pass: creates the Deployment, waits on its readiness.
			_, err = reconcileOnce(gwRec, "gw")
			Expect(err).NotTo(HaveOccurred())
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "gw-router", Namespace: ns}, dep)).To(Succeed())

			// Fake the router pods becoming ready (no kubelet in envtest).
			dep.Status.Replicas = 3
			dep.Status.ReadyReplicas = 3
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			// Final pass: joins the peers to the network and programs.
			_, err = reconcileOnce(gwRec, "gw")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gw), gw)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(gw.Status.Conditions, string(gwv1.GatewayConditionProgrammed))).To(BeTrue())

			routers, err := nbClient.Networks.Routers(network.Status.NetworkID).List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(routers).To(HaveLen(1))
		})
	})
})
