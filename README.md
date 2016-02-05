
Horcrux - On Demand, Version controlled access to your Data
============================================================

Why Horcrux?
------------
* Horcrux provides you (developer) a local copy of the whole centralized Data (database etc), so you can develop/test your application (native or containerized) without worrying about messing up your precious central copy.
* Centralized copy can be located anywhere (local servers with scp, minio etc. access) or in cloud (aws s3, azure, google cloud etc.) so you are free to access it from anywhere (within your office, at home, in-flight (just kidding)...
* It is visible as a local FUSE filesystem.
* Only portion of data accessed is retrieved and stored locally, so you dont have to buy terabytes of storage for your development/test machine.
* Lets you modify the data locally and view what is changed (only the meta data, not the actual binary file diff - coming soon...).
In future versions, it will be sorta like git, so you can:
* Commit the local changes, and push it to your centralized repo with a comment (only modified portion is pushed).
* Browse through changes and
* Access (mount) any version of remote data locally (roll back/forward) and can troubleshoot issues with ease.
* Bestof all, you don't have to do any evil spell (just a few good ones).

We would like to call it a git for DB (but technically it is not the same :-), see note below), since it will provide all git compatible commands (may even provide a git extension) so you can do pretty much all things with your data that you are already doing with git for your source code.

Getting started
----------------
1. Install Horcrux
  * Get the binary copy:
    - (not yet, pls use the src)
  * or source code:
    - http://github.com/muthu-r/horcrux

2. To Generate Horcrux for your data, you need to do the "Reducto" spell on the system with data (good time to do a backup now :)

  * horcrux-cli generate \<name\> \<in-dir\> \<out-dir\>
  
  ```
    Example for a database (MySQL in this case) with AMCC data:
    - horcrux-cli generate AMCC /var/lib/mysql/amcc /usr/share/horcrux/amcc
  ```
  * Check and verify locally using CP access type
  
  ```
    - horcrux-cli mount AMCC cp:///usr/share/horcrux/amcc /mnt 
  ```
  
  <blockquote>
    NOTE: full path after "cp://", so three slashes in total
  </blockquote>

  * Check your /mnt and make sure you can access it (may be run some sha1sum on this and the original copy to check...)
  * Put it in any central place (or more than one, if you want more flexibility)
 
   ```
    - Example, for AWS S3, you can use "aws s3 sync" on /use/share/horcrux/amcc
   ```

3. To access it locally:
 * Install Horcrux - Please see above.
 * Install fuse:
    - Should be installed default in most distros
    - If not, check your distro doc on how to 
  and configure it:
    
    ```
      - Add "allow_other" to /etc/fuse.conf
      - chmod a+r /etc/fuse.conf
    ```
    <blockquote>
     NOTE: Right now we use "Allow Other" FUSE mount option so the mount point can be accessed by any user within the local system.
        We need this so the apps within containers can access the bind mountpoint. It may not be a problem right now since developers
        machine are mostly used by one (or few trusted) user(s). Will be addressed in future release.
    </blockquote>

 * Do the "Revelo" spell
    - For non-container access, use the horcrux-cli to mount the central copy locally:
    
      ```
      - horcrux-cli mount <name> <access type> <local mount point>
      ```
    
      - Access types can be any of scp, minio, s3 in current version (azure, google cloud - next version)
      Examples, for accessing AMCC horcrux generated in step 2.
        - with SCP
          
          ```
            horcrux-cli mount AMCC scp://muthu@central-store:/usr/share/horcrux/amcc /mnt/amcc
          ```
          <blockquote>
            uses passwd less acess. If you want to provide passwd, use --access=scp://user::passwd@host:/full-path. NOTE: not secure :)
          </blockquote>  
        - with Minio using bucket horcrux.amcc (credentials in $HOME/.minio/horcrux.json - please see sample file below)
          
          ```
            horcrux-cli mount AMCC minio://central-store:9000/horcrux.amcc /mnt/amcc
          ```
        - with AWS S3, using bucket horcrux.amcc in us-west-1 region (credentials in $HOME/.aws/credentials)
          
          ```
            horcrux-cli mount AMCC s3://horcrux.amcc@us-west-1 /mnt/amcc
          ```
    
    Cache is located at $HOME/.horcrux/AMCC.
    <blockquote>
    NOTE: If cache gets bigger you can manually clean-up files inside there to free up space. It will be retrieved from central repo next time. Just make sure
      its not a locally modified copy before "rm -rf" it :)
    </blockquote>
    
    - For containerized development, use horcrux-dv as a volume plugin
      - Run horcrux-dv as root (as we need to create sock in /run/docker/plugins also we will use /run/horcrux for our local storage). We will register as docker volume plugin.
      - Create docker volumes using horcrux volume plugin, and using the right access type option
        Examples, for accessing AMCC horcrux (generated above):
        - with SCP
          
          ```
            docker volume create -d horcrux --name v1 -o --name=AMCC -o --access=scp://muthu@central-store:/usr/share/horcrux/amcc
          ```
          <blockquote>
            (uses passwd less access. If you want to provide passwd, use --access=scp://user::passwd@host:/full-path>
            NOTE: not secure :)
          </blockquote>
            
        - with minio using bucket horcrux.amcc (credentials in /root/.minio/horcrux.json - please see sample file below)
          
            ```
              docker volume create -d horcrux --name v1 -o --name=AMCC -o --access=minio://central-store:9000/horcrux.amcc
            ```
        - with AWS S3, using bucket horcrux.amcc in us-west-1 region (credentials in /root/.aws/credentials):
          
            ```
              docker volume create -d horcrux --name v1 -o --name=AMCC -o --access=s3://horcrux.amcc@us-west-1
            ```
      - Once you have the volume "v1" created, you can use it in containers:
          
        ```
          docker run -it -v v1:/data ubuntu:latest /bin/bash
        ```
        and volume is visible inside the container at /data
    
    <blockquote>
    NOTE: With scp access, volumes are not visible inside the container consistently. If you experience this, you can workaround by creating a temp container with the volume
      and leave it running while you create/manage other containers
    </blockquote>
    
    Cache is located at /run/horcrux/v1
    
    <blockquote>
    NOTE: If cache gets bigger you can manually clean-up files inside there to free up space. It will be retrieved from central repo next time when aaccessed. Just make sure
      its not a locally modified copy before "rm -rf" it :)
    </blockquote>
    
4. That's pretty much it... 

Happy hacking!!

Muthukumar - m u t h u r AT g m a i l DOT c o m

ACKNOWLEDMENTS
--------------
Many thanks to:
 * Docker folks for the excellent volume plugin interface and the documents - and answering my questions in #docker-dev 
 * Bazil (http://github.com/bazil) folks for their Go FUSE on top of which this is built.
 * Minio team (http://github.com/minio) for their excellent object store server.
 * An excellent doc on scp protocol:
    - https://blogs.oracle.com/janp/entry/how_the_scp_protocol_works
    - and https://gist.github.com/jedy/3357393 for pointing it
 * And the golang folks.

Known Issues:
-------------
 * This is version 00.01, so that pretty much explains it (but not bad at all, give it a shot and let me know)
 * Only tested on Linux systems (latest version of Fedora, Ubuntu, CentOS)
 * Docker version 1.9 till 1.10 (where the docker behavior is changed for volume plugins - for good, btw)
    - will soon have 00.02 with Docker 1.10+ support
 * Some of local FS calls (Rename, Setattr) is not there yet - just a matter of adding it, let me know if its needed badly :)
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
* Version 00.01
  - Supported access methods - CP, SCP, MINIO, AWS S3
  - Tested only on Linux systems (Fedora, Ubuntu, CentOS)
  - Versioning is not yet there (coming soon...)
