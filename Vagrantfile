# -*- mode: ruby -*-
# vi: set ft=ruby :

Vagrant.configure("2") do |config|
  config.vm.define "ubuntu", primary: true do |cfg|
    cfg.vm.box = "bento/ubuntu-18.04"
    cfg.vm.provision "shell", inline: <<-SHELL
      apt-get update
      apt-get install -y exim4 devscripts dh-make dh-systemd libsystemd-dev fakeroot
      snap install go --classic
    SHELL
  end

  config.vm.define "freebsd" do |cfg|
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
