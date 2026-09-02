"""Build real certificate chains for the trust and TLS tests.

Generated rather than checked in: a real chain committed as a fixture
expires, and a test that starts failing on a date is worse than no test.
Everything these produce is a genuine, correctly-formed certificate, so
the verification paths under test are the real ones.
"""

import datetime

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID


def _name(cn):
    return x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, cn)])


def make_cert(cn, issuer_cert=None, issuer_key=None, ca=False, dns=None, days=365):
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.UTC)
    subject = _name(cn)
    issuer = issuer_cert.subject if issuer_cert else subject
    signing_key = issuer_key or key
    builder = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(issuer)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(days=1))
        .not_valid_after(now + datetime.timedelta(days=days))
        .add_extension(x509.BasicConstraints(ca=ca, path_length=None), critical=True)
        # Real CA-issued certificates carry these, and OpenSSL needs them to
        # build a chain: without an Authority Key Identifier the handshake
        # fails with "Missing Authority Key Identifier" rather than for the
        # reason a test is actually probing.
        .add_extension(
            x509.SubjectKeyIdentifier.from_public_key(key.public_key()), critical=False
        )
    )
    # A CA certificate must assert keyCertSign, and an end-entity one must
    # assert the usages a TLS server needs; OpenSSL rejects a chain whose CA
    # omits key usage entirely.
    if ca:
        usage = x509.KeyUsage(
            digital_signature=True,
            key_cert_sign=True,
            crl_sign=True,
            content_commitment=False,
            key_encipherment=False,
            data_encipherment=False,
            key_agreement=False,
            encipher_only=False,
            decipher_only=False,
        )
    else:
        usage = x509.KeyUsage(
            digital_signature=True,
            key_encipherment=True,
            key_cert_sign=False,
            content_commitment=False,
            data_encipherment=False,
            key_agreement=False,
            crl_sign=False,
            encipher_only=False,
            decipher_only=False,
        )
    builder = builder.add_extension(usage, critical=True)
    if not ca:
        builder = builder.add_extension(
            x509.ExtendedKeyUsage([x509.oid.ExtendedKeyUsageOID.SERVER_AUTH]),
            critical=False,
        )

    authority_key = issuer_key.public_key() if issuer_key else key.public_key()
    builder = builder.add_extension(
        x509.AuthorityKeyIdentifier.from_issuer_public_key(authority_key),
        critical=False,
    )
    if dns:
        builder = builder.add_extension(
            x509.SubjectAlternativeName([x509.DNSName(d) for d in dns]), critical=False
        )
    cert = builder.sign(signing_key, hashes.SHA256())
    return cert, key


def pem(*certs):
    return "".join(c.public_bytes(serialization.Encoding.PEM).decode() for c in certs)
