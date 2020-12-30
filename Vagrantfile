# -*- mode: ruby -*-
# vi: set ft=ruby :

Vagrant.configure("2") do |config|
  config.vm.box = "bento/ubuntu-16.04"
  config.vm.provision "shell", inline: <<-SHELL
    apt-get update
    apt-get install -y exim4 devscripts dh-make dh-systemd libsystemd-dev fakeroot
    snap install go --classic
  SHELL
end
