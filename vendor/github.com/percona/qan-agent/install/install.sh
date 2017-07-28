#!/bin/bash

# This script is mostly a wrapper around the percona-qan-agent-installer binary
# which does the heay lifting: creating API resources, configuring service, etc.

set -u

error() {
   echo "ERROR: $1" >&2
   exit 1
}

# Set up variables.
# BASEDIR here must match BASEDIR in percona-qan-agent sys-init script.
BIN="percona-qan-agent"
BASEDIR="${BASEDIR:-"/usr/local/percona/qan-agent"}"
INIT_SCRIPT="/etc/init.d/$BIN"
INSTALLER_DIR=$(dirname $0)

print_usage() {
   echo "  -uninstall"
   printf "\tStop agent and uninstall it\n"
   echo "  -help -h -?"
   printf "\tPrint help and exit\n"
   echo
   echo "Usage: install [options] API_HOST[:PORT]"
   echo "  * Options are listed above and must be specified first"
   echo "  * Specify the -basedir option to change the local install directory"
   echo "  * API_HOST is the hostname where Percona Platform Datastore is running"
   echo
}

if [ "$*" = "--help" -o "$*" = "-help" -o "$*" = "-h" -o "$*" = "-?" -o "$*" = "help" ]; then
   "$INSTALLER_DIR/bin/$BIN-installer" -h
   print_usage
   exit 0
fi

# Print usage if no cmd line args; at least API_KEY[:PORT] is required.
if [ $# -eq 0 ]; then
   "$INSTALLER_DIR/bin/$BIN-installer" -h
   print_usage
   exit 1
fi

# ###########################################################################
# Sanity checks and setup
# ###########################################################################

# Check if script is run as root as we need write access to /etc, /usr/local
if [ $EUID -ne 0 ]; then
   error "$BIN install requires root user; detected effective user ID $EUID"
fi

# Check compatibility
KERNEL=`uname -s`
if [ "$KERNEL" != "Linux" -a "$KERNEL" != "Darwin" ]; then
   error "$BIN only runs on Linux; detected $KERNEL"
fi

PLATFORM=`uname -m`
if [ "$PLATFORM" != "x86_64" -a "$PLATFORM" != "i686" -a "$PLATFORM" != "i386" ]; then
   error "$BIN supports only x86_64 and i686 platforms; detected $PLATFORM"
fi

echo "Platform: $KERNEL $PLATFORM"

# Parse BASEDIR from -basedir if specified.
basedir="$(echo "$*" | perl -ne '/-basedir[= ]*(\S+)/ && print $1')"
[ "$basedir" ] && BASEDIR="$basedir"
echo "Basedir: $BASEDIR"

# ###########################################################################
# Version comparision
# https://gist.github.com/livibetter/1861384
# ###########################################################################
_ver_cmp_1() {
  (( "10#$1" == "10#$2" )) && return 0
  (( "10#$1" >  "10#$2" )) && return 1
  (( "10#$1" <  "10#$2" )) && return 2
  exit 1
}

ver_cmp() {
  local A B i result
  A=(${1//./ })
  B=(${2//./ })
  i=0
  while (( i < ${#A[@]} )) && (( i < ${#B[@]})); do
    _ver_cmp_1 "${A[i]}" "${B[i]}"
    result=$?
    [[ $result =~ [12] ]] && return $result
    let i++
  done
  _ver_cmp_1 "${#A[i]}" "${#B[i]}"
  return $?
}

upgrade() {
   if [ "$KERNEL" != "Darwin" ]; then
       ${INIT_SCRIPT} stop
   else
       echo "killall $BIN"
   fi

   # Install agent binary
   cp -f "$INSTALLER_DIR/bin/$BIN" "$BASEDIR/bin/"

   # Copy init script (for backup, as we are going to install it in /etc/init.d)
   cp -f "$INSTALLER_DIR/init.d/$BIN" "$BASEDIR/init.d/"

   if [ "$KERNEL" != "Darwin" ]; then
      cp -f "$BASEDIR/init.d/$BIN" "/etc/init.d/"
      chmod a+x "/etc/init.d/$BIN"
      ${INIT_SCRIPT} start
      echo "Upgrade complete."
   else
      echo "Upgrade complete. To run on Mac OS:"
      echo "  cd $BASEDIR ; sudo ./bin/$BIN -basedir ."
   fi
}

install() {
    # ###########################################################################
    # Check if already installed and upgrade if needed
    # ###########################################################################
    currentVersion=""
    currentRev=""
    if [ -x "$BASEDIR/bin/$BIN" ]; then
       currentVersion="$("$BASEDIR/bin/$BIN" -version | cut -f2 -d" ")"
       currentRev="$(echo "$currentVersion" | cut -d. -f4)"
    fi

    newVersion=$("$INSTALLER_DIR/bin/$BIN" -version | cut -f2 -d" ")
    newRev="$(echo "$newVersion" | cut -d. -f4)"

    if [ "$currentRev" -o "$newRev" ]; then
       if [ -x "$BASEDIR/bin/$BIN" ]; then
          echo "Upgrading to dev build $newVersion"
          upgrade
          exit 0
       fi
       echo "Installing to dev build $newVersion"
    else
        echo "Version provided by installer: $newVersion"
        if [ "$currentVersion" ]; then
           echo "Version currently installed: $currentVersion"
           cmpVer=0
           ver_cmp "$currentVersion" "$newVersion" || cmpVer=$?
           if [ "$cmpVer" == "2" -o "$rev" ]; then
              echo "Upgrading to $newVersion"
              upgrade
              exit 0
           elif [ "$cmpVer" == "1" ]; then
               echo "Newer version already installed, exiting."
               exit 1
           else
               echo "Same version already installed, exiting."
               exit 1
           fi
        fi
    fi

    # ###########################################################################
    # Create dir structure if not exist
    # ###########################################################################

    mkdir -p "$BASEDIR/"{bin,init.d} \
        || error "'mkdir -p $BASEDIR/{bin,init.d}' failed"

    # ###########################################################################
    # Run installer and forward all remaining parameters to it with "$@"
    # ###########################################################################

    "$INSTALLER_DIR/bin/$BIN-installer" -basedir "$BASEDIR" $@
    exitStatus=$?
    if [ "$exitStatus" -eq "10" ]; then
       print_usage
       exit 1
    elif [ "$exitStatus" -ne "0" ]; then
       echo
       error "Failed to install $BIN"
    fi

    # ###########################################################################
    # Install sys-int script and percona-qan-agent binary
    # ###########################################################################

    # Install agent binary
    cp -f "$INSTALLER_DIR/bin/$BIN" "$BASEDIR/bin/"

    # Copy init script (for backup, as we are going to install it in /etc/init.d)
    cp -f "$INSTALLER_DIR/init.d/$BIN" "$BASEDIR/init.d/"

    "$BASEDIR/bin/$BIN" -basedir "$BASEDIR" -ping >/dev/null
    if [ $? -ne 0 ]; then
       error "Installed $BIN but ping test failed"
    fi

    if [ "$KERNEL" != "Darwin" ]; then
       cp -f "$BASEDIR/init.d/$BIN" "/etc/init.d/"
       chmod a+x "/etc/init.d/$BIN"

       # Check if the system has chkconfig or update-rc.d.
       if hash update-rc.d 2>/dev/null; then
               echo "Using update-rc.d to install $BIN service"
               update-rc.d  $BIN defaults >/dev/null
       elif hash chkconfig 2>/dev/null; then
               echo "Using chkconfig to install $BIN service"
               chkconfig $BIN on >/dev/null
       else
          echo "Cannot find chkconfig or update-rc.d.  $BIN is installed but"
          echo "it will not restart automatically with the server on reboot.  Please"
          echo "email the follow to cloud-tools@percona.com:"
          cat /etc/*release
       fi

       ${INIT_SCRIPT} start
       if [ $? -ne 0 ]; then
          error "Failed to start $BIN"
       fi
    else
       echo "Mac OS detected, not installing sys-init script."
    fi

    # ###########################################################################
    # Cleanup
    # ###########################################################################
    if [ "$KERNEL" != "Darwin" ]; then
        echo   
        echo "Success! $BIN $newVersion is installed and running."
        echo
    else
        echo
        echo "Success! $BIN $newVersion is installed. To run on Mac OS:"
        echo "  cd $BASEDIR ; sudo ./bin/$BIN -basedir ."
        echo
    fi
    exit 0
}

uninstall() {
    # ###########################################################################
    # Stop agent and uninstall sys-int script
    # ###########################################################################
    if [ "$KERNEL" != "Darwin" ]; then
       if [ -x "$INIT_SCRIPT" ]; then
           echo "Stopping agent ..."
           ${INIT_SCRIPT} stop
           if [ $? -ne 0 ]; then
              error "Failed to stop $BIN"
           fi
       fi

       echo "Uninstalling sys-init script ..."
       # Check if the system has chkconfig or update-rc.d.
       if hash update-rc.d 2>/dev/null; then
               echo "Using update-rc.d to uninstall $BIN service"
               update-rc.d -f "$BIN" remove
       elif hash chkconfig 2>/dev/null; then
               echo "Using chkconfig to uninstall $BIN service"
               chkconfig --del "$BIN"
       else
          echo "Cannot find chkconfig or update-rc.d.  $BIN is installed but"
          echo "it will not restart automatically with the server on reboot.  Please"
          echo "email the follow to cloud-tools@percona.com:"
          cat /etc/*release
       fi

       # Remove init script
       echo "Removing $INIT_SCRIPT ..."
       rm -f "$INIT_SCRIPT"
    else
       echo "Mac OS detected, no sys-init script. To stop $BIN:"
       echo "killall $BIN"
    fi

    # ###########################################################################
    # Uninstall percona-qan-agent
    # ###########################################################################

    echo "Removing $BASEDIR..."
    [ -d "$BASEDIR" ] && rm -rf "$BASEDIR"
    echo "$BIN uninstall successful"
    exit 0
}

[[ $* == *-uninstall* ]] && uninstall
install $@
