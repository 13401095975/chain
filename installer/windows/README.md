# windows-installer

These instructions assume that your PATH includes the wix tools binary. My wix tools binary is located at `C:\Program Files (x86)\WiX Toolset v3.10\bin`.

### Dependencies 

The Postgres Installer can be downloaded from http://www.enterprisedb.com/products-services-training/pgdownload

The VC++ Redistributable can be downloaded from https://www.microsoft.com/en-us/download/search.aspx?q=redistributable+package&first=11

Make sure you have the 64-bit versions. Chain Core Windows does not support 32-bit.

### Build

The chain bundler is capable of building multiple .msi's and .exe's into a single installer .exe. 

First, build the chain core msi. To do this, from inside of `installer/windows` run: 

```
cd ChainPackage
candle -ext WixHttpExtension -ext WixUtilExtension ChainCoreInstaller.wxs
```

This generates `ChainPackage/ChainCoreInstaller.wixobj`

Next, run 

`light -ext WixHttpExtension -ext WixUtilExtension ChainCoreInstaller.wixobj`

to generate `ChainPackage/ChainCoreInstaller.msi`. 

Next, build the Chain Bundle. Run 

```
cd ../ChainBundle
candle Bundle.wxs \
  -ext WixBalExtension \
  -dChainPackage.TargetPath='Z:\chain\installer\windows\ChainPackage\ChainCoreInstaller.msi' \
  -dPostgresPackage.TargetPath='Z:\chain\installer\windows\Postgres\postgresql-9.5.4-2-windows-x64.exe'
```
(but obviously sub out your path for my target paths) 

This generates `ChainBundle/Bundle.wixobj`. Next, run

`light Bundle.wixobj -ext WixBalExtension`

This generates Bundle.exe in your current working directory. Clicking on Bundle.exe will install Chain Core as an application on your PC. 
