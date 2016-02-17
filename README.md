
Horcrux - On Demand, Version controlled access to your Data
============================================================

About Horcrux
--------------
Docker containers offer developers with the agility and flexibility of replicating the production/test setup in their development environment. So now, developers can develop features, unit test, fix issues in their local setup before pushing it to the test/production environment. Since containers are state-less, it can be moved anywhere easily, say within datacenter or into clouds etc. But in most cases, containers has to access/modify data that traditionally lives in a centralized storage. For example, one of the popular container stack LEMP, needs to access MySQL database. In order to access these data, Docker developed the concept of volume plugins, which can be associated with a container when it is created.

Now the container can move to a different location as long as the volume plugin works there as well. This solves the problem in production/test cases, but if developers have to access the centralized data, that again restricts their flexibily. At the same time, with ever increasing size of data, it is not possible to give each developer a separate copy of the data (say database). That would be prohibitively expensive. To solve this, we give you __"Horcrux"__...


What Horcrux provides?
------------
* Horcrux provides you (developer) a local view of the whole centralized Data (database etc), so you can develop/test your application without worrying about messing up your precious central repository.
* Centralized repository can be located anywhere (local servers that provide scp access, [Minio](http://minio.io) servers etc.) or in cloud (Amazon AWS S3, Microsoft Azure, Google Cloud etc.), so you are free to access it from anywhere (within your office, at home, in-flight (just kidding)...
* The data volume is visible as a local FUSE filesystem in the developer/test environment.
* When the data is accessed by the application (containers), only the particular chunk of data needed is fetched from the remote repository on-demand and stored locally (in the cache). The whole access is transparent to the applicaiton/container.
* Since only portion of data accessed is retrieved and stored locally, you don't have to buy terabytes of storage for each developers setup or test machine.
* When the working data set is accessed next time around, it is served from the local cache, blazingly fast (almost, as fast as the local file system/storage :))
* The local view provided by Horcrux is a read/write view, so the application/container can modify the data locally.
* and can view, at any time, what is changed.

In future versions, we will add git like capabilities, so you can:
* Commit the local changes, and push it to the centralized repo with a comment (only modified portion is pushed).
* Browse through changes in the remote repository and,
* Access (mount) any version of remote data locally (roll back/forward) to develop/ troubleshoot issues with ease.
* Bestof all, you don't have to do any evil spell (just a few good ones).

We would like to call it a git for DB (but technically it is not the same :), since it will provide all git compatible commands (may even provide a git extension) so you can do pretty much all things with your data that you are already doing with git for your source code.

Getting started
================
Steps Overview:

1. Install Horcrux

2. Generate a Horcrux version for your central data

3. Place the Horcrux version of your data anywhere you like (local servers within your LAN, AWS S3 etc). We suggest putting it in more than one place. If you don't know yet, check out a cool project, [Minio](http://minio.io) object store server. It can be used to store Horcrux as well.

4. In the development or test environment: Create Docker volumes using Horcrux volume driver and specifying where the remote data is stored

5. Now the volumes can be used within your containers as data volumes.

#To Generate Horcrux version of the data:
### Step 1: Install Horcrux
Horcrux consists of two binaries, _horcrux-cli_ and _horcrux-dv_

* horcrux-cli: Used to generate Horcrux version of the data.
* horcrux-dv: A volume driver plugin for Docker.

Download the latest binary copy of __horcrux-cli__ from:

####For Linux
----------
 * [64-bit Intel/AMD](https://github.com/muthu-r/horcrux/tree/master/release/latest/linux/x86_64)

####For OSX
--------
* [64-bit Darwin](https://github.com/muthu-r/horcrux/tree/master/release/latest/OSX/x86_64)
<blockquote>
NOTE: Need help to generate binaries for OSX
</blockquote>

####From Source Code:
---------------------
* [Horcrux GitHub](https://github.com/muthu-r/horcrux)

### Step 2: Generate Horcrux version of the data [Reducto Spell]

#### horcrux-cli generate
```
NAME:
   ./horcrux-cli generate - [options] <name> <in-dir> <out-dir>

USAGE:
   ./horcrux-cli generate [command options] [arguments...]

OPTIONS:
   --chunksize, -s "64M"	Chunk Size

```
Lets consider an example of MySQL database stored in database server __"kural"__. We name the database as "AMCC" (some meaningful name).
- Original Database Server: __kural__
- Name of the database: __AMCC__
- Location of MySQL files: __/var/lib/mysql__
    <blockquote>
    For this example we use all files including log files :)
    </blockquote>
![alt text][Generate]

#### [Optional] Validate the generated Horcrux (in the Database server):
- Server used to validate: __kural__
- Mount point used: __/mnt/horcrux__
- Horcrux location: __/opt/horcrux__
- Access Method: __Local "cp"__ _(a.k.a Locket)_
     <blockquote>
     NOTE: full path after "cp://", so three slashes in total
     </blockquote>
![alt text][Local Mount]

#### Distribute Horcrux to remote repositories
* For AWS S3, you can use "aws s3 sync" on /opt/horcrux-amcc
* You can also replicate the Horcrux to a local server and give SSH access to your developers

That's it... now _Horcrux_ can be accessed by multiple developers simultaneously, and with minimal storage in their development/test machines.

# Steps to do to work with the Horcrux generated above
If a developer wants to access the Horcrux, he/she needs to do the "Revelo" spell as follows
### Step 1: Install Horcrux
* Please refer to Install section above

### Step 2: Install and Configure FUSE
* FUSE should be installed default in most distros (Ubuntu/Fedora/CentOS/RHEL). If not, please check your distro doc on how to install FUSE packages.
* Edit __/etc/fuse.conf__ and add _"user_allow_other"_ line
   ```
   user_allow_other
   ```
   <blockquote>
   NOTE: Right now we use "Allow Other" FUSE mount option so the mount point can be accessed by any user within the local system. We need this so the apps within containers can access the bind mountpoint. It may not be a problem right now since developers machine are mostly used by one (or few trusted) user(s). Will be addressed in future release.
  </blockquote>
* Make /etc/fuse.conf readable
   ```
   # chmod a+r /etc/fuse.conf
   ```

### Step 3: Start horcrux-dv volume plugin
   ```
   # horcrux-dv >& /var/log/horcrux-dv.log &
   ```
### Step 4: Create Docker volumes using Horcrux Volume driver

* Docker volume __"v1"__ that uses SCP access from remote server __kural__

   ```
   - Here docker volume name is "v1"

   - "-d": specifies the Docker volume driver as horcrux

   - We use the -o to pass options to our horcrux volume driver

   - first option: "--name=AMCC", here we give the same name that was used in generate step

   - second option: "--access=scp://muthu@kural:/opt/horcrux-mysql-amcc" specifies the access method as SCP and the remote location as "kural:/opt/horcrux-mysql-amcc"
   ```

* Docker volume __"v2"__ that uses AWS S3 as remote location

  ```
  - Here the bucket name is "muthu.horcrux"

  - Region is "us-west-1"
  ```
   <blockquote>
   NOTE: AWS credentials are in ~/.aws/credentials
   </blockquote>

 ![alt text][Docker Volume Create]

### Step 5: Create Docker containers with volume v1
- Create container test-scp using volume v1
![alt text][Docker Container Create V1]

- Inspect container __test-scp__ for mount points
![alt text][Docker Container Inspect V1]


[Generate]: https://github.com/muthu-r/horcrux/blob/master/Docs/Generate.png
[Local Mount]: https://github.com/muthu-r/horcrux/blob/master/Docs/LocalMount.png
[Docker Volume Create]: https://github.com/muthu-r/horcrux/blob/master/Docs/DockerVolumeCreate.png
[Docker Container Create V1]: https://github.com/muthu-r/horcrux/blob/master/Docs/DockerContainerCreateV1.png
[Docker Container Inspect V1]: https://github.com/muthu-r/horcrux/blob/master/Docs/DockerContainerInspectV1.png

Container test-scp can now access the all MySQL files inside /data directory

## That's pretty much it...

Happy hacking!!

Muthukumar. R - m u t h u r AT g m a i l DOT c o m

ACKNOWLEDGEMENTS
----------------
Many thanks to:
 * [Docker folks](http://docker.io) for the excellent volume plugin interface and the documents - and answering my questions in #docker-dev
 * [Bazil folks](http://github.com/bazil/fuse) for their Go FUSE on top of which this is built.
 * [Minio team](https://minio.io) for their excellent object store server.
 * An excellent doc on scp protocol:
    - https://blogs.oracle.com/janp/entry/how_the_scp_protocol_works
    - and https://gist.github.com/jedy/3357393 for pointing it
 * And the [golang](https://golang.org) folks.

Known Issues:
-------------
 * This is version 00.03-rc, so that pretty much explains it (but not bad at all, give it a shot and let me know)
 * Only tested on Linux systems (latest version of Fedora, Ubuntu, CentOS)
 * Some of local FS calls (Rename) is not there yet - just a matter of adding it, let me know if its needed badly :)
 * Open flags like O_EXCL is b0rked (not hard to fix though, next version)
 * With scp access, volumes are not visible inside the container consistently. If you experience this, you can workaround by creating a temp container with that volume
   and leave it running while you create/manage other containers for that volume.
 * Cache is not cleaned up after "docker volume rm". If it grows big, please clean it up manually for now.

Need Help:
----------
 * Use it and let me know if it helps you..
 * Help in supporting other access methods (Google Cloud, Azure, etc..)
 * More testing and bug reports (see reporting issues)
 * Mac support
 * E m a i l m e : m u t h u r AT g m a i l DOT c o m

Release Notes:
--------------
* Version 00.02 (02/06/2016)
  - Support for Docker 1.10+ volume plugin changes (Get, List)

* Version 00.01 (02/05/2016)
  - Supported access methods - CP, SCP, MINIO, AWS S3
  - Tested only on Linux systems (Fedora, Ubuntu, CentOS)
  - Versioning is not yet there (coming soon...)
