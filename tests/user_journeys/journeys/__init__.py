"""Marks `journeys` as a package.

Not decoration. Without an __init__.py in both this directory and
tests_lib/, pytest's default `prepend` import mode derives a test module's
name from its BASENAME alone, so `journeys/test_storage.py` and
`tests_lib/test_storage.py` both claim the module name `test_storage` and a
bare `pytest` from the suite root dies at collection with "import file
mismatch". The pixi tasks scope to one directory each and so never hit it,
which makes the failure look like a broken checkout to anyone running
pytest by hand. With these files the modules are `journeys.test_storage`
and `tests_lib.test_storage`, which cannot collide.
"""
