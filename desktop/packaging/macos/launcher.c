#include <mach-o/dyld.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int parent_directory(char *path) {
    char *slash = strrchr(path, '/');
    if (slash == NULL || slash == path) {
        return -1;
    }
    *slash = '\0';
    return 0;
}

int main(int argc, char **argv) {
    (void)argc;
    char executable[PATH_MAX];
    uint32_t size = (uint32_t)sizeof(executable);
    if (_NSGetExecutablePath(executable, &size) != 0) {
        fputs("Snow Desktop: application path is too long\n", stderr);
        return 70;
    }
    char resolved[PATH_MAX];
    if (realpath(executable, resolved) == NULL || parent_directory(resolved) != 0 ||
        parent_directory(resolved) != 0) {
        perror("Snow Desktop: resolve application bundle");
        return 70;
    }

    char snow[PATH_MAX];
    char desktop[PATH_MAX];
    if (snprintf(snow, sizeof(snow), "%s/Resources/snow", resolved) >= (int)sizeof(snow) ||
        snprintf(desktop, sizeof(desktop), "%s/MacOS/snow-desktop-bin", resolved) >=
            (int)sizeof(desktop)) {
        fputs("Snow Desktop: bundled executable path is too long\n", stderr);
        return 70;
    }
    if (getenv("SNOW_BINARY") == NULL && setenv("SNOW_BINARY", snow, 1) != 0) {
        perror("Snow Desktop: set SNOW_BINARY");
        return 70;
    }
    if (getenv("SNOW_PROJECT") == NULL) {
        const char *home = getenv("HOME");
        if (home == NULL || home[0] == '\0' || setenv("SNOW_PROJECT", home, 1) != 0) {
            fputs("Snow Desktop: HOME is required when SNOW_PROJECT is unset\n", stderr);
            return 70;
        }
    }
    argv[0] = desktop;
    execv(desktop, argv);
    perror("Snow Desktop: start bundled client");
    return 70;
}
