import base64
from unittest.mock import MagicMock, patch

import pytest
from kubernetes.client.rest import ApiException
from kubernetes.config import ConfigException

from nebari_journeys import constants
from nebari_journeys.cluster import LONGHORN_STORAGE_CLASS, Cluster


def _secret(data: dict[str, str]):
    s = MagicMock()
    s.data = {k: base64.b64encode(v.encode()).decode() for k, v in data.items()}
    return s


def _svc(ip=None, hostname=None):
    ingress = MagicMock()
    ingress.ip = ip
    ingress.hostname = hostname
    svc = MagicMock()
    svc.status.load_balancer.ingress = [ingress]
    item_list = MagicMock()
    item_list.items = [svc]
    return item_list


def _cluster(core=None, custom=None):
    return Cluster(core=core or MagicMock(), custom=custom or MagicMock())


def test_secret_value_is_base64_decoded():
    core = MagicMock()
    core.read_namespaced_secret.return_value = _secret({"admin-password": "s3cret"})
    assert _cluster(core).secret_value("keycloak", "x", "admin-password") == "s3cret"


def test_secret_value_raises_a_useful_error_on_missing_key():
    core = MagicMock()
    core.read_namespaced_secret.return_value = _secret({"other": "v"})
    with pytest.raises(KeyError, match="admin-password"):
        _cluster(core).secret_value("keycloak", "x", "admin-password")


def test_keycloak_admin_password_reads_the_pinned_secret_and_key():
    core = MagicMock()
    core.read_namespaced_secret.return_value = _secret({"admin-password": "pw"})
    assert _cluster(core).keycloak_admin_password() == "pw"
    core.read_namespaced_secret.assert_called_once_with(
        name=constants.KEYCLOAK_ADMIN_SECRET, namespace=constants.KEYCLOAK_NAMESPACE
    )


def test_realm_admin_password_reads_the_pinned_secret_and_key():
    core = MagicMock()
    core.read_namespaced_secret.return_value = _secret({"password": "rpw"})
    assert _cluster(core).realm_admin_password() == "rpw"
    core.read_namespaced_secret.assert_called_once_with(
        name=constants.REALM_ADMIN_SECRET, namespace=constants.KEYCLOAK_NAMESPACE
    )


def test_gateway_address_prefers_ip():
    core = MagicMock()
    core.list_namespaced_service.return_value = _svc(
        ip="10.0.0.5", hostname="lb.example"
    )
    assert _cluster(core).gateway_address() == "10.0.0.5"


def test_gateway_address_falls_back_to_hostname():
    core = MagicMock()
    core.list_namespaced_service.return_value = _svc(hostname="lb.example")
    assert _cluster(core).gateway_address() == "lb.example"


def test_gateway_address_uses_the_pinned_namespace_and_selector():
    core = MagicMock()
    core.list_namespaced_service.return_value = _svc(ip="1.2.3.4")
    _cluster(core).gateway_address()
    core.list_namespaced_service.assert_called_once_with(
        namespace=constants.GATEWAY_NAMESPACE,
        label_selector=constants.GATEWAY_LABEL_SELECTOR,
    )


def test_has_app_is_true_when_present():
    custom = MagicMock()
    custom.list_namespaced_custom_object.return_value = {
        "items": [{"metadata": {"name": "keycloak"}}]
    }
    assert _cluster(custom=custom).has_app("keycloak") is True


def test_has_app_is_false_when_absent():
    custom = MagicMock()
    custom.list_namespaced_custom_object.return_value = {"items": []}
    assert _cluster(custom=custom).has_app("longhorn-backup") is False


def test_require_app_skips_when_absent():
    custom = MagicMock()
    custom.list_namespaced_custom_object.return_value = {"items": []}
    with pytest.raises(pytest.skip.Exception, match="longhorn-backup"):
        _cluster(custom=custom).require_app("longhorn-backup")


def test_connect_error_names_both_the_kubeconfig_and_incluster_failures():
    with (
        patch(
            "nebari_journeys.cluster.kubeconfig.load_kube_config",
            side_effect=ConfigException("bad kubeconfig"),
        ),
        patch(
            "nebari_journeys.cluster.kubeconfig.load_incluster_config",
            side_effect=ConfigException("no service host"),
        ),
        pytest.raises(ConfigException) as excinfo,
    ):
        Cluster.connect()
    assert "bad kubeconfig" in str(excinfo.value)
    assert "no service host" in str(excinfo.value)


def test_default_storage_class_prefers_the_annotated_default():
    storage = MagicMock()
    sc = MagicMock()
    sc.metadata.name = "custom"
    sc.metadata.annotations = {"storageclass.kubernetes.io/is-default-class": "true"}
    storage.list_storage_class.return_value = MagicMock(items=[sc])
    cluster = Cluster(core=MagicMock(), custom=MagicMock(), storage=storage)
    assert cluster.default_storage_class() == "custom"


def test_default_storage_class_falls_back_to_longhorn():
    storage = MagicMock()
    sc = MagicMock()
    sc.metadata.name = "other"
    sc.metadata.annotations = {}
    storage.list_storage_class.return_value = MagicMock(items=[sc])
    cluster = Cluster(core=MagicMock(), custom=MagicMock(), storage=storage)
    assert cluster.default_storage_class() == "longhorn"


def _storage(names):
    storage = MagicMock()
    classes = []
    for name in names:
        sc = MagicMock()
        sc.metadata.name = name
        sc.metadata.annotations = {}
        classes.append(sc)
    storage.list_storage_class.return_value = MagicMock(items=classes)
    return storage


def _longhorn_cluster(names):
    return Cluster(core=MagicMock(), custom=MagicMock(), storage=_storage(names))


def test_has_longhorn_is_true_when_the_longhorn_storage_class_exists():
    cluster = _longhorn_cluster(["gp3", LONGHORN_STORAGE_CLASS])
    assert cluster.has_longhorn() is True


def test_has_longhorn_is_false_on_a_cluster_without_it():
    """Longhorn core is not an ArgoCD Application (there is no
    apps/longhorn.yaml, only apps/longhorn-backup.yaml), so require_app cannot
    answer this and the StorageClass is the signal."""
    cluster = _longhorn_cluster(["gp2", "gp3"])
    assert cluster.has_longhorn() is False


def test_require_longhorn_skips_and_names_what_was_missing():
    cluster = _longhorn_cluster(["gp2"])
    with pytest.raises(pytest.skip.Exception, match=LONGHORN_STORAGE_CLASS):
        cluster.require_longhorn()


def test_require_longhorn_does_not_skip_when_longhorn_is_present():
    cluster = _longhorn_cluster([LONGHORN_STORAGE_CLASS])
    assert cluster.require_longhorn() is None


def _gateway(listeners):
    return {"spec": {"listeners": listeners}}


def _https_listener(name="tls-secret", namespace=None):
    ref = {"name": name, "kind": "Secret"}
    if namespace is not None:
        ref["namespace"] = namespace
    return {"name": "https", "tls": {"mode": "Terminate", "certificateRefs": [ref]}}


def test_gateway_tls_secret_ref_reads_the_gateways_certificate_ref():
    custom = MagicMock()
    custom.get_namespaced_custom_object.return_value = _gateway(
        [{"name": "http"}, _https_listener(name="operator-supplied-tls")]
    )
    cluster = _cluster(custom=custom)
    assert cluster.gateway_tls_secret_ref() == (
        "operator-supplied-tls",
        constants.GATEWAY_NAMESPACE,
    )


def test_gateway_tls_secret_ref_honours_a_cross_namespace_secret():
    custom = MagicMock()
    custom.get_namespaced_custom_object.return_value = _gateway(
        [_https_listener(name="platform-tls", namespace="platform-certs")]
    )
    cluster = _cluster(custom=custom)
    assert cluster.gateway_tls_secret_ref() == ("platform-tls", "platform-certs")


def test_gateway_tls_secret_ref_falls_back_to_the_default_without_a_gateway():
    custom = MagicMock()
    custom.get_namespaced_custom_object.side_effect = ApiException(
        status=404, reason="Not Found"
    )
    cluster = _cluster(custom=custom)
    assert cluster.gateway_tls_secret_ref() == (
        constants.GATEWAY_TLS_SECRET,
        constants.GATEWAY_NAMESPACE,
    )


def test_gateway_tls_secret_ref_falls_back_when_no_listener_terminates_tls():
    custom = MagicMock()
    custom.get_namespaced_custom_object.return_value = _gateway([{"name": "http"}])
    cluster = _cluster(custom=custom)
    assert cluster.gateway_tls_secret_ref() == (
        constants.GATEWAY_TLS_SECRET,
        constants.GATEWAY_NAMESPACE,
    )


def _route(hostnames, parent=None):
    return {
        "spec": {
            "parentRefs": [{"name": parent or constants.GATEWAY_NAME}],
            "hostnames": hostnames,
        }
    }


def _routes_cluster(routes, certificate=None):
    custom = MagicMock()
    custom.list_cluster_custom_object.return_value = {"items": routes}
    if certificate is None:
        custom.get_namespaced_custom_object.side_effect = ApiException(
            status=404, reason="Not Found"
        )
    else:
        custom.get_namespaced_custom_object.return_value = certificate
    return _cluster(custom=custom)


def test_domain_comes_from_the_httproutes_attached_to_the_gateway():
    """The default shape: cert-manager mints the certificate and the routes
    are argocd./keycloak./longhorn. of the platform domain."""
    cluster = _routes_cluster(
        [_route(["argocd.nebari.example"]), _route(["keycloak.nebari.example"])],
        certificate={"spec": {"commonName": "nebari.example"}},
    )
    assert cluster.domain() == "nebari.example"


def test_domain_includes_the_bare_apex_landing_route():
    """The real-world shape that broke the old strip-one-label heuristic: the
    landing page route serves the bare apex (no service label), one fewer
    label than argocd./keycloak./longhorn., so the domain must be computed as
    the longest common suffix, not by stripping a fixed number of labels."""
    cluster = _routes_cluster(
        [
            _route(["argocd.dcmcand-333.openteams.dev"]),
            _route(["keycloak.dcmcand-333.openteams.dev"]),
            _route(["longhorn.dcmcand-333.openteams.dev"]),
            _route(["dcmcand-333.openteams.dev"]),
            _route([], parent=constants.GATEWAY_NAME),  # http-to-https-redirect
        ],
        certificate={"spec": {"commonName": "dcmcand-333.openteams.dev"}},
    )
    assert cluster.domain() == "dcmcand-333.openteams.dev"


def test_domain_works_on_local_kind_with_no_apex_route():
    cluster = _routes_cluster(
        [
            _route(["argocd.nebari.local"]),
            _route(["keycloak.nebari.local"]),
            _route(["longhorn.nebari.local"]),
        ],
        certificate={"spec": {"commonName": "nebari.local"}},
    )
    assert cluster.domain() == "nebari.local"


def test_domain_apex_only_single_hostname_yields_itself():
    cluster = _routes_cluster(
        [_route(["dcmcand-333.openteams.dev"])],
        certificate={"spec": {"commonName": "dcmcand-333.openteams.dev"}},
    )
    assert cluster.domain() == "dcmcand-333.openteams.dev"


def test_domain_works_with_an_operator_supplied_certificate():
    """certificate.type: existing means gateway-certificate.yaml is never
    rendered (pkg/argocd/writer.go, skipCertificateTemplate), so the
    Certificate does not exist. That must not be fatal: the derived domain is
    used as-is."""
    cluster = _routes_cluster(
        [_route(["argocd.nebari.example"]), _route(["keycloak.nebari.example"])]
    )
    assert cluster.domain() == "nebari.example"


def test_domain_ignores_routes_attached_to_another_gateway():
    cluster = _routes_cluster(
        [
            _route(["argocd.nebari.example"]),
            _route(["keycloak.nebari.example"]),
            _route(["app.someone-elses.example"], parent="other-gateway"),
        ],
        certificate={"spec": {"commonName": "nebari.example"}},
    )
    assert cluster.domain() == "nebari.example"


def test_domain_ignores_wildcard_hostnames():
    cluster = _routes_cluster(
        [
            _route(["*.nebari.example"]),
            _route(["argocd.nebari.example"]),
            _route(["keycloak.nebari.example"]),
        ],
        certificate={"spec": {"commonName": "nebari.example"}},
    )
    assert cluster.domain() == "nebari.example"


def test_domain_skips_a_route_with_no_hostnames():
    """The http-to-https-redirect HTTPRoute attaches to the gateway but
    carries no hostnames at all; it must not affect the result."""
    cluster = _routes_cluster(
        [
            _route(["argocd.nebari.example"]),
            _route(["keycloak.nebari.example"]),
            _route([]),
        ],
        certificate={"spec": {"commonName": "nebari.example"}},
    )
    assert cluster.domain() == "nebari.example"


def test_domain_falls_back_to_the_certificate_when_no_route_names_a_domain():
    cluster = _routes_cluster(
        [], certificate={"spec": {"commonName": "nebari.example"}}
    )
    assert cluster.domain() == "nebari.example"


def test_domain_error_names_what_it_looked_for_instead_of_raising_a_bare_404():
    cluster = _routes_cluster([])
    with pytest.raises(ValueError) as excinfo:
        cluster.domain()
    message = str(excinfo.value)
    assert constants.GATEWAY_NAME in message
    assert constants.GATEWAY_CERTIFICATE_NAME in message
    assert "HTTPRoute" in message


def test_domain_refuses_to_guess_when_a_route_is_from_a_foreign_domain():
    """An operator route under a completely different domain collapses the
    common suffix to fewer than two labels; the message must list the
    hostnames it saw so the cause is obvious."""
    cluster = _routes_cluster(
        [
            _route(["argocd.nebari.example"]),
            _route(["keycloak.nebari.example"]),
            _route(["something.example.org"]),
        ]
    )
    with pytest.raises(ValueError) as excinfo:
        cluster.domain()
    message = str(excinfo.value)
    assert "argocd.nebari.example" in message
    assert "something.example.org" in message


def test_domain_character_wise_suffix_confusion_is_rejected():
    """`evil-openteams.dev` must not be treated as sharing the
    `openteams.dev` suffix beyond the shared `dev` label: comparison is by
    DNS label, not by character."""
    cluster = _routes_cluster(
        [
            _route(["argocd.dcmcand-333.openteams.dev"]),
            _route(["evil-openteams.dev"]),
        ]
    )
    with pytest.raises(ValueError) as excinfo:
        cluster.domain()
    message = str(excinfo.value)
    assert "argocd.dcmcand-333.openteams.dev" in message
    assert "evil-openteams.dev" in message


def test_domain_agrees_with_a_matching_certificate_common_name():
    cluster = _routes_cluster(
        [_route(["argocd.nebari.example"]), _route(["keycloak.nebari.example"])],
        certificate={"spec": {"commonName": "nebari.example"}},
    )
    assert cluster.domain() == "nebari.example"


def test_domain_raises_when_the_certificate_disagrees_with_the_derived_domain():
    cluster = _routes_cluster(
        [_route(["argocd.nebari.example"]), _route(["keycloak.nebari.example"])],
        certificate={"spec": {"commonName": "someone-elses.example"}},
    )
    with pytest.raises(ValueError) as excinfo:
        cluster.domain()
    message = str(excinfo.value)
    assert "nebari.example" in message
    assert "someone-elses.example" in message


def test_domain_error_is_clear_when_the_httproute_crd_is_absent():
    """A cluster without the Gateway API must produce the same named error,
    not a raw 404 from the API server."""
    custom = MagicMock()
    custom.list_cluster_custom_object.side_effect = ApiException(
        status=404, reason="Not Found"
    )
    custom.get_namespaced_custom_object.side_effect = ApiException(
        status=404, reason="Not Found"
    )
    cluster = _cluster(custom=custom)
    with pytest.raises(ValueError, match="HTTPRoute"):
        cluster.domain()


def test_domain_propagates_a_permissions_error_on_httproutes():
    custom = MagicMock()
    custom.list_cluster_custom_object.side_effect = ApiException(
        status=403, reason="Forbidden"
    )
    cluster = _cluster(custom=custom)
    with pytest.raises(ApiException):
        cluster.domain()
