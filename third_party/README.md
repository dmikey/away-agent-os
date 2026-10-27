# Vendored Virtualization.framework bindings

`vz/` is Code-Hex/vz v3.8.0, retained with its upstream license. Its Objective-C `AppDelegate` class is renamed to `AwayVZAppDelegate` in the graphics implementation and test harness. Wails defines its own `AppDelegate`; an Objective-C class name is global to the binary, and the unmodified packages fail to link together. No virtualization, memory, network or guest API behavior is changed.

Upstream: https://github.com/Code-Hex/vz/tree/v3.8.0
