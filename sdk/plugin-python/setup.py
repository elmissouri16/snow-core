"""Compatibility metadata for older pip/setuptools frontends.

Modern builds read pyproject.toml. Keeping this tiny fallback makes the private
local SDK installable with the Python 3.9 tooling shipped by older macOS hosts.
"""

from setuptools import setup


setup(
    name="snow-plugin",
    version="0.1.0.dev0",
    description="Local Python SDK for authoring Snow protocol-v2 plugins",
    python_requires=">=3.9",
    packages=["snow_plugin"],
    package_dir={"snow_plugin": "src/snow_plugin"},
    package_data={"snow_plugin": ["py.typed"]},
)
