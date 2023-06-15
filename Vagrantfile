# -*- mode: ruby -*-
# vi: set ft=ruby :

Vagrant.configure("2") do |config|
  config.vm.define "linux", primary: true do |cfg|
    cfg.vm.box = "bento/ubuntu-22.04"
    cfg.vm.provision "shell", inline: <<-SHELL
      apt-get update
      apt-get install -y ca-certificates make libsystemd-dev docker.io exim4
      echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' > /etc/apt/sources.list.d/goreleaser.list
      apt-get update
      apt-get install -y goreleaser
    SHELL
  end

  config.vm.define "freebsd", autostart: false do |cfg|
    cfg.vm.box = "bento/freebsd-11"
    cfg.vm.provision "shell", inline: <<-SHELL
      export ASSUME_ALWAYS_YES=yes
      pkg-static bootstrap -f
      pkg upgrade -f
      pkg install -y rsync exim go git
      echo 'exim_enable="YES"' >> /etc/rc.conf
      /usr/local/etc/rc.d/exim start
    SHELL
    cfg.vm.synced_folder ".", "/vagrant", type: "rsync", rsync__exclude: ".git/"
  end
end
