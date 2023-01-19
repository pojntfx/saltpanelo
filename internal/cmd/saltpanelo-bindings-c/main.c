#include "libsaltpanelo.h"
#include <pthread.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

void *handle_adapter_link(void *adapter) {
  char *rv = SaltpaneloAdapterLink(adapter);
  if (strcmp(rv, "") != 0) {
    fprintf(stderr, "Error in SaltpaneloAdapterLink: %s\n", rv);

    exit(1);
  }

  return NULL;
}

struct SaltpaneloOnRequestCallResponse handle_on_request_call(char *src_id,
                                                       char *src_email,
                                                       char *route_id,
                                                       char *channel_id) {
  printf("Called on_request_call");

  struct SaltpaneloOnRequestCallResponse rv = {.Accept = true, .Err = ""};

  return rv;
}

int main() {
  void *adapter = SaltpaneloNewAdapter(
      &handle_on_request_call, NULL, NULL, NULL, "ws://localhost:1338", "127.0.0.1",
      false, 10000, "https://pojntfx.eu.auth0.com/",
      "dIFKbQTQhqAWd3AKmeAwXt87tIL6bkcv", "http://localhost:11337");

  char *rv = SaltpaneloAdapterLogin(adapter);
  if (strcmp(rv, "") != 0) {
    fprintf(stderr, "Error in SaltpaneloAdapterLogin: %s\n", rv);

    return 1;
  }

  pthread_t adapter_linker;
  if (pthread_create(&adapter_linker, NULL, handle_adapter_link, adapter) !=
      0) {
    perror("Error in pthread_create");

    return 1;
  }

  if (pthread_join(adapter_linker, NULL) != 0) {
    perror("Error in pthread_join");

    return 1;
  }

  while (1) {
    printf("Email to call: ");

    char *email = NULL;
    size_t email_len = 0;
    getline(&email, &email_len, stdin);

    printf("Channel ID to call: ");

    char *channel_id = NULL;
    size_t channel_id_len = 0;
    getline(&channel_id, &channel_id_len, stdin);

    struct SaltpaneloAdapterRequestCall_return rv =
        SaltpaneloAdapterRequestCall(adapter, email, channel_id);
    if (strcmp(rv.r1, "") != 0) {
      fprintf(stderr, "Error in SaltpaneloAdapterRequestCall: %s\n", rv.r1);

      return 1;
    }

    if (rv.r0 == 1) {
      printf("Callee accepted the call\n");
    } else {
      printf("Callee denied the call\n");
    }
  }

  return 0;
}