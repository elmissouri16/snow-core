"""Compatibility metadata for older pip/setuptools frontends.

Modern builds read pyproject.toml. Keeping this tiny fallback makes the private
local SDK installable with the Python 3.9 tooling shipped by older macOS hosts.
"""

from setuptools import setup


setup(
    name="snow-plugin",
    version="0.1.0.dev0",
    description="Local Python SDK for authoring Snow protocol-v2 plugins",
    license="MIT",
    python_requires=">=3.9",
    classifiers=[
        "Development Status :: 2 - Pre-Alpha",
        "License :: OSI Approved :: MIT License",
    ],
    packages=["snow_plugin"],
    package_dir={"snow_plugin": "src/snow_plugin"},
    package_data={"snow_plugin": ["py.typed"]},
)
