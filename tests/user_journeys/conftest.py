"""Root conftest.

This file is deliberately minimal: it exists only to register
--keep-namespace. Pytest only picks up pytest_addoption from a conftest
at the rootdir, so this must live here even though it has no fixtures.

The fixtures that actually need a live cluster live in
journeys/conftest.py, one directory down. An autouse, session-scoped
fixture defined here would apply to every subdirectory, including
tests_lib/, which must stay runnable with no cluster available.
"""

def pytest_addoption(parser):
    parser.addoption(
        "--keep-namespace",
        action="store_true",
        default=False,
        help="Do not delete scratch namespaces, for debugging a failed journey.",
    )
