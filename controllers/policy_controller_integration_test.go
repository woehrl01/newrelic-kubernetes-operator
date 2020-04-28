// +build integration

package controllers

import (
	"context"
	"errors"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/newrelic/newrelic-client-go/pkg/alerts"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	nrv1 "github.com/newrelic/newrelic-kubernetes-operator/api/v1"
	"github.com/newrelic/newrelic-kubernetes-operator/interfaces"
	"github.com/newrelic/newrelic-kubernetes-operator/interfaces/interfacesfakes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("policy reconciliation", func() {
	var (
		ctx            context.Context
		r              *PolicyReconciler
		policy         *nrv1.Policy
		request        ctrl.Request
		namespacedName types.NamespacedName
		conditionName types.NamespacedName
		//expectedEvents []string
		//secret        *v1.Secret
		fakeAlertFunc func(string, string) (interfaces.NewRelicAlertsClient, error)
	)

	BeforeEach(func() {
		ctx = context.Background()

		alertsClient = &interfacesfakes.FakeNewRelicAlertsClient{}

		fakeAlertFunc = func(string, string) (interfaces.NewRelicAlertsClient, error) {
			return alertsClient, nil
		}

		alertsClient.CreatePolicyStub = func(a alerts.Policy) (*alerts.Policy, error) {
			a.ID = 333
			return &a, nil
		}

		r = &PolicyReconciler{
			Client:          k8sClient,
			Log:             logf.Log,
			AlertClientFunc: fakeAlertFunc,
		}

		policy = &nrv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-policy",
				Namespace: "default",
			},
			Spec: nrv1.PolicySpec{
				Name:               "test policy",
				APIKey:             "112233",
				IncidentPreference: "PER_POLICY",
				Conditions: []nrv1.NrqlAlertCondition{
					{
						Spec: nrv1.NrqlAlertConditionSpec{
							Terms: []nrv1.AlertConditionTerm{
								{
									Duration:     resource.MustParse("30"),
									Operator:     "above",
									Priority:     "critical",
									Threshold:    resource.MustParse("5"),
									TimeFunction: "all",
								},
							},
							Nrql: nrv1.NrqlQuery{
								Query:      "SELECT 1 FROM MyEvents",
								SinceValue: "5",
							},
							Type:                "NRQL",
							Name:                "NRQL Condition",
							RunbookURL:          "http://test.com/runbook",
							ValueFunction:       "max",
							ID:                  777,
							ViolationCloseTimer: 60,
							ExpectedGroups:      2,
							IgnoreOverlap:       true,
							Enabled:             true,
							ExistingPolicyID:    42,
							APIKey:              "api-key",
							Region:              "us",
						},
						Status: nrv1.NrqlAlertConditionStatus{
							AppliedSpec: &nrv1.NrqlAlertConditionSpec{},
						},
					},
				},
			},
			Status: nrv1.PolicyStatus{
				AppliedSpec: &nrv1.PolicySpec{},
				PolicyID:    0,
			},
		}

		namespacedName = types.NamespacedName{
			Namespace: "default",
			Name:      "test-policy",
		}
		conditionName = types.NamespacedName{
			Namespace: "default",
			Name:      "test-policy1942898816", //TODO: swap with calling hash function
		}
		request = ctrl.Request{NamespacedName: namespacedName}

	})

	Context("When starting with no policies", func() {
		Context("when creating a valid policy", func() {
			It("should create that policy", func() {

				err := k8sClient.Create(ctx, policy)
				Expect(err).ToNot(HaveOccurred())

				// call reconcile
				_, err = r.Reconcile(request)
				Expect(err).ToNot(HaveOccurred())

			})

			It("updates the policyId on the Policy resource", func() {

				err := k8sClient.Create(ctx, policy)
				Expect(err).ToNot(HaveOccurred())

				// call reconcile
				_, err = r.Reconcile(request)
				Expect(err).ToNot(HaveOccurred())

				var endStatePolicy nrv1.Policy
				err = k8sClient.Get(ctx, namespacedName, &endStatePolicy)
				Expect(err).To(BeNil())
				Expect(endStatePolicy.Status.PolicyID).To(Equal(333))

			})
		})
		Context("when the New Relic API returns an error", func() {
			BeforeEach(func() {
				alertsClient.CreatePolicyStub = func(alerts.Policy) (*alerts.Policy, error) {
					return &alerts.Policy{}, errors.New("Any Error Goes Here")
				}
			})

			It("should not update the PolicyID", func() {

				createErr := k8sClient.Create(ctx, policy)
				Expect(createErr).ToNot(HaveOccurred())

				// call reconcile
				_, reconcileErr := r.Reconcile(request)
				Expect(reconcileErr).To(HaveOccurred())

				var endStatePolicy nrv1.Policy
				getErr := k8sClient.Get(ctx, namespacedName, &endStatePolicy)
				Expect(getErr).ToNot(HaveOccurred())
				Expect(endStatePolicy.Status.PolicyID).To(Equal(0))

			})
		})

		Context("when creating a valid policy with conditions", func() {
			It("should create the conditions", func() {

				err := k8sClient.Create(ctx, policy)
				Expect(err).ToNot(HaveOccurred())

				// call reconcile
				_, err = r.Reconcile(request)
				Expect(err).ToNot(HaveOccurred())

				//call reconcile a second time to create conditions
				//_, err = r.Reconcile(request)
				//Expect(err).ToNot(HaveOccurred())

				var endStateNrqlCondition nrv1.NrqlAlertCondition
				err = k8sClient.Get(ctx, conditionName, &endStateNrqlCondition)
				Expect(err).ToNot(HaveOccurred())
				Expect(endStateNrqlCondition.Spec.Name).To(Equal("NRQL Condition"))

			})

			//It("updates the policyId on the Policy resource", func() {
			//
			//	err := k8sClient.Create(ctx, policy)
			//	Expect(err).ToNot(HaveOccurred())
			//
			//	// call reconcile
			//	_, err = r.Reconcile(request)
			//	Expect(err).ToNot(HaveOccurred())
			//
			//	var endStatePolicy nrv1.Policy
			//	err = k8sClient.Get(ctx, namespacedName, &endStatePolicy)
			//	Expect(err).To(BeNil())
			//	Expect(endStatePolicy.Status.PolicyID).To(Equal(333))
			//
			//})
		})

		AfterEach(func() {
			// Delete the policy
			err := k8sClient.Delete(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// Need to call reconcile to delete finalizer
			_, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
		})

	})

	Context("When starting with an existing policy", func() {
		BeforeEach(func() {

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())
			// call reconcile
			_, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())

			Expect(alertsClient.CreatePolicyCallCount()).To(Equal(1))
			Expect(alertsClient.UpdatePolicyCallCount()).To(Equal(0))

			// change the event after creation via reconciliation
			err = k8sClient.Get(ctx, namespacedName, policy)
			Expect(err).ToNot(HaveOccurred())
		})
		Context("and deleting that policy", func() {
			It("should successfully delete", func() {
				err := k8sClient.Delete(ctx, policy)
				Expect(err).ToNot(HaveOccurred())

				// Need to call reconcile to delete finalizer
				_, err = r.Reconcile(request)
				Expect(err).ToNot(HaveOccurred())

				var endStatePolicy nrv1.Policy
				err = k8sClient.Get(ctx, namespacedName, &endStatePolicy)
				Expect(err).NotTo(BeNil())


			})
			It("should delete the condition", func() {
				err := k8sClient.Delete(ctx, policy)
				Expect(err).ToNot(HaveOccurred())

				// Need to call reconcile to delete finalizer
				_, err = r.Reconcile(request)
				Expect(err).ToNot(HaveOccurred())

				var endStateCondition nrv1.NrqlAlertCondition
				err = k8sClient.Get(ctx, conditionName, &endStateCondition)

				Expect(err).To(HaveOccurred())
				Expect(endStateCondition.Spec.Name).ToNot(Equal(policy.Spec.Conditions[0].Spec.Name))

			})
		})
		Context("and New Relic API returns a 404", func() {
			BeforeEach(func() {
				alertsClient.DeletePolicyStub = func(int) (*alerts.Policy, error) {
					return &alerts.Policy{}, errors.New("Imaginary 404 Failure")
				}
			})
			It("should succeed as if a previous reconcile already deleted the policy", func() {
			})
			AfterEach(func() {
				alertsClient.DeletePolicyStub = func(int) (*alerts.Policy, error) {
					return &alerts.Policy{}, nil
				}
				// Delete the policy
				err := k8sClient.Delete(ctx, policy)
				Expect(err).ToNot(HaveOccurred())

				// Need to call reconcile to delete finalizer
				_, err = r.Reconcile(request)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Context("When starting with an existing policy", func() {
		BeforeEach(func() {

			err := k8sClient.Create(ctx, policy)
			Expect(err).ToNot(HaveOccurred())
			// call reconcile
			_, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())

			Expect(alertsClient.CreatePolicyCallCount()).To(Equal(1))
			Expect(alertsClient.UpdatePolicyCallCount()).To(Equal(0))

			// change the event after creation via reconciliation
			err = k8sClient.Get(ctx, namespacedName, policy)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("and updating that policy", func() {
			BeforeEach(func() {
				alertsClient.UpdatePolicyStub = func(alerts.Policy) (*alerts.Policy, error) {
					return &alerts.Policy{
						ID: 222,
					}, nil
				}
				policy.Spec.IncidentPreference = "PER_CONDITION_AND_TARGET"
			})

			It("should successfully update", func() {
				err := k8sClient.Update(ctx, policy)
				Expect(err).ToNot(HaveOccurred())

				// Need to call reconcile to update the condition
				_, err = r.Reconcile(request)
				Expect(err).ToNot(HaveOccurred())

				var endStatePolicy nrv1.Policy
				err = k8sClient.Get(ctx, namespacedName, &endStatePolicy)
				Expect(err).To(BeNil())
				//Expect(endStatePolicy.Status.PolicyID).To(Equal(222))
				Expect(alertsClient.UpdatePolicyCallCount()).To(Equal(1))

			})

		})

		Context("and when the alerts client returns an error", func() {
			BeforeEach(func() {
				alertsClient.UpdatePolicyStub = func(alerts.Policy) (*alerts.Policy, error) {
					return &alerts.Policy{}, errors.New("oh no")
				}
				policy.Spec.IncidentPreference = "PER_CONDITION_AND_TARGET"
			})
			It("should return an error", func() {
				err := k8sClient.Update(ctx, policy)
				Expect(err).ToNot(HaveOccurred())

				// Need to call reconcile to update the condition
				_, err = r.Reconcile(request)
				Expect(err).To(HaveOccurred())
			})
		})

		AfterEach(func() {
			err := k8sClient.Delete(ctx, policy)
			Expect(err).ToNot(HaveOccurred())

			// Need to call reconcile to delete finalizer
			_, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
		})

	})

})
