class Ecsfgrun < Formula
  desc "AWS assume role credential wrapper"
  homepage "https://github.com/masahide/ecsfgrun"
  url "https://github.com/masahide/ecsfgrun/releases/download/v0.3.0/ecsfgrun_Darwin_x86_64.tar.gz"
  version "0.3.0"
  sha256 "5e9beda2b4af5798433853f9b1a03dec8e3ef05d8a32f6833b31fcb49ce83cb0"

  def install
    bin.install "ecsfgrun"
  end

  test do
    system "#{bin}/ecsfgrun -v"
  end
end
