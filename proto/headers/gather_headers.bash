#!/usr/bin/bash

# cmd="grep https://git.kernel.org/pub/scm/linux/kernel/git/torvalds xtcppb.proto"
# echo "${cmd}"
# eval "${cmd}"

cd headers || exit
wget https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/plain/include/linux/time64.h
wget https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/plain/include/linux/time64.h
wget https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/plain/include/uapi/linux/inet_diag.h
wget https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/plain/net/core/sock.c
wget https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/plain/include/uapi/linux/tcp.h

# das@t:~/Downloads/xtcp/pkg/xtcppb$ ls ./headers/ -la
# total 832
# drwxrwxr-x 2 das das   4096 Jun 13 12:15 .
# drwxrwxr-x 3 das das   4096 Jun 13 11:31 ..
# -rw-rw-r-- 1 das das  36943 Jun 13 12:15 inet_diag.h
# -rw-rw-r-- 1 das das 684943 Jun 13 12:15 sock.c
# -rw-rw-r-- 1 das das  75924 Jun 13 12:15 tcp.h
# -rw-rw-r-- 1 das das  35433 Jun 13 12:15 time64.h

# https://github.com/cheshirekow/protostruct
# add-apt-repository ppa:josh-bialkowski/tangent
# apt install protostruct
