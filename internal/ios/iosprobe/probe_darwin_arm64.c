#include <stdlib.h>
#include <string.h>

long add(long a, long b) { return a + b; }

long slen(const char *s) { return (long)strlen(s); }

long via_malloc(long n) {
    long *p = (long *)malloc(sizeof(long));
    long r = n;
    if (p != NULL) {
        p[0] = n;
        r = p[0];
        free(p);
    }
    return r;
}
