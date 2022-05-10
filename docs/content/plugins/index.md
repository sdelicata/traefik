---
title: "Traefik Plugins Documentation"
description: "Learn how to use Traefik Plugins. Read the technical documentation."
---

# Traefik Plugins

Plugins are a powerful feature for extending Traefik with custom features and behaviors.

You can browse community-contributed plugins from the catalog in the [Plugin Catalog](https://plugins.traefik.io/).

To add a new plugin to a Traefik instance, you must modify that instance's static configuration.
The code to be added is provided for you when you choose **Install the Plugin** from the Plugin Catalog.
To learn more about Traefik plugins, consult the [documentation](https://plugins.traefik.io/install).

!!! danger "Experimental Features"
    Plugins can potentially modify the behavior of Traefik in unforeseen ways.
    Exercise caution when adding new plugins to production Traefik instances.

## Build Your Own Plugins

Traefik users can create their own plugins and contribute them to the Plugin Catalog to share them with the community.

Traefik plugins are loaded dynamically. 
They need not be compiled, and no complex toolchain is necessary to build them. 
The experience of implementing a Traefik plugin is comparable to writing a web browser extension.

To learn more and see code for example Traefik plugins, please see the [developer documentation](https://plugins.traefik.io/create).
