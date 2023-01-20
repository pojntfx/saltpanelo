#include "libsaltpanelo.h"
#include <callback.h>
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

struct example_external_data {};

void on_request_call_handler(void *ptr, struct vacall_alist *alist) {
  struct example_external_data *example_data = ptr;

  char *src_id = va_arg_ptr(alist, char *);
  char *src_email = va_arg_ptr(alist, char *);
  char *route_id = va_arg_ptr(alist, char *);
  char *channel_id = va_arg_ptr(alist, char *);

  struct SaltpaneloOnRequestCallResponse *rv =
      va_arg_ptr(alist, struct SaltpaneloOnRequestCallResponse *);

  printf("Call with src ID %s, src email %s, route ID %s and channel ID %s "
         "requested and accepted\n",
         src_id, src_email, route_id, channel_id);

  rv->Accept = true;
  rv->Err = "";
}

void on_call_disconnected_handler(void *ptr, struct vacall_alist *alist) {
  struct example_external_data *example_data = ptr;

  char *route_id = va_arg_ptr(alist, char *);
  char **rv = va_arg_ptr(alist, char **);

  printf("Call with route ID %s disconnected\n", route_id);

  *rv = "";
}

void on_handle_call_handler(void *ptr, struct vacall_alist *alist) {
  struct example_external_data *example_data = ptr;

  char *route_id = va_arg_ptr(alist, char *);
  char *raddr = va_arg_ptr(alist, char *);
  char **rv = va_arg_ptr(alist, char **);

  printf("Call with route ID %s and remote address %s started\n", route_id,
         raddr);

  *rv = "";
}

void open_url_handler(void *ptr, struct vacall_alist *alist) {
  struct example_external_data *example_data = ptr;

  char *url = va_arg_ptr(alist, char *);
  char **rv = va_arg_ptr(alist, char **);

  printf("Open the following URL in your browser: %s\n", url);

  *rv = "";
}

int main() {
  struct example_external_data example_data = {};

  void *handle_on_request_call =
      alloc_callback(&on_request_call_handler, &example_data);
  void *handle_on_call_disconnected =
      alloc_callback(&on_call_disconnected_handler, &example_data);
  void *handle_on_handle_call =
      alloc_callback(&on_handle_call_handler, &example_data);
  void *handle_open_url = alloc_callback(&open_url_handler, &example_data);

  void *adapter = SaltpaneloNewAdapter(
      handle_on_request_call, handle_on_call_disconnected,
      handle_on_handle_call, handle_open_url, "ws://localhost:1338",
      "127.0.0.1", false, 10000, "https://pojntfx.eu.auth0.com/",
      "An94hvwzqxMmFcL8iEpTVrd88zFdhVdl", "http://localhost:11337");

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

  while (1) {
    printf("Email to call: ");

    char *email = NULL;
    size_t email_len = 0;
    getline(&email, &email_len, stdin);

    email[strcspn(email, "\n")] = 0;

    printf("Channel ID to call: ");

    char *channel_id = NULL;
    size_t channel_id_len = 0;
    getline(&channel_id, &channel_id_len, stdin);

    channel_id[strcspn(channel_id, "\n")] = 0;

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

  if (pthread_join(adapter_linker, NULL) != 0) {
    perror("Error in pthread_join");

    return 1;
  }

  return 0;
}