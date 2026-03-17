// Copyright 2020 The Nakama Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import {Component, Injectable, OnDestroy, OnInit} from '@angular/core';
import {
  Router,
  ActivatedRoute,
  NavigationCancel,
  NavigationEnd,
  NavigationError,
  NavigationStart,
  CanActivate,
  CanActivateChild,
  ActivatedRouteSnapshot, RouterStateSnapshot,
} from '@angular/router';
import {bufferTime, distinctUntilChanged} from 'rxjs/operators';
import {Subscription} from 'rxjs';
import {AuthenticationService} from '../authentication.service';
import {NgbNavChangeEvent} from '@ng-bootstrap/ng-bootstrap';
import {SegmentService} from 'ngx-segment-analytics';
import {ConsoleService, UserRole} from '../console.service';
import {Globals} from '../globals';
import {environment} from '../../environments/environment';
import { TranslationService } from '../services/translation.service';

@Component({
  templateUrl: './base.component.html',
  styleUrls: ['./base.component.scss'],
})
export class BaseComponent implements OnInit, OnDestroy {
  protected readonly UserRole = UserRole;
  private routerSub: Subscription;
  private segmentRouterSub: Subscription;
  public loading = true;
  public error = '';
  public currentLang: string = 'en';
  public userMenuOpen = false;

  public routes = [
    // Top: dashboard
    {navItem: 'status', routerLink: ['/status'], label: 'Dashboard', section: 'top', minRole: '', icon: 'status'},

    // Main menu
    {navItem: 'accounts', routerLink: ['/accounts'], label: 'Players', section: 'main', minRole: 'ACCOUNT', icon: 'accounts'},
    {navItem: 'groups', routerLink: ['/groups'], label: 'Groups', section: 'main', minRole: 'GROUP', icon: 'groups'},
    {navItem: 'storage', routerLink: ['/storage'], label: 'Storage', section: 'main', minRole: 'STORAGE_DATA', icon: 'storage'},
    {navItem: 'matches', routerLink: ['/matches'], label: 'Matches', section: 'main', minRole: 'MATCH', icon: 'running-matches'},
    {navItem: 'leaderboards', routerLink: ['/leaderboards'], label: 'Leaderboards', section: 'main', minRole: 'LEADERBOARD', icon: 'leaderboard'},
    {navItem: 'notifications', routerLink: ['/notifications'], label: 'Notifications', section: 'main', minRole: 'NOTIFICATION', icon: 'notification'},
    {navItem: 'purchases', routerLink: ['/purchases'], label: 'Payments', section: 'main', minRole: 'IN_APP_PURCHASE', icon: 'purchases'},
    {navItem: 'chat', routerLink: ['/chat'], label: 'Chat Messages', section: 'main', minRole: 'CHANNEL_MESSAGE', icon: 'chat'},
    // Extra main items from your extensions
    {navItem: 'announcements', routerLink: ['/announcements'], label: 'Announcements', section: 'main', minRole: 'ANNOUNCEMENT', icon: 'announcement'},
    {navItem: 'system-notifications', routerLink: ['/system-notifications'], label: 'SystemNotice', section: 'main', minRole: 'SYSTEM_NOTIFICATION', icon: 'system-notifications'},
    {navItem: 'personal-notifications', routerLink: ['/personal-notifications'], label: 'PersonalNotice', section: 'main', minRole: 'NOTIFICATION', icon: 'gift'},
    {navItem: 'vip-accounts', routerLink: ['/vip-accounts'], label: 'VIPMANAGER', section: 'main', minRole: 'VIP_MANAGER', icon: 'vip'},

    // Development
    {navItem: 'users', routerLink: ['/users'], label: 'User Management', section: 'dev', minRole: 'USER', icon: 'user-management'},
    {navItem: 'apiexplorer', routerLink: ['/apiexplorer'], label: 'API Explorer', section: 'dev', minRole: 'API_EXPLORER', icon: 'api-explorer'},
    {navItem: 'modules', routerLink: ['/modules'], label: 'Runtime Modules', section: 'dev', minRole: 'CONFIGURATION', icon: 'runtime-modules'},
    {navItem: 'config', routerLink: ['/config'], label: 'Settings', section: 'dev', minRole: 'CONFIGURATION', icon: 'configuration'},
    {navItem: 'auditlog', routerLink: ['/audit/log'], label: 'Audit Log', section: 'dev', minRole: 'AUDIT_LOG', icon: 'status'},
  ];

  constructor(
    private readonly route: ActivatedRoute,
    private readonly router: Router,
    private segment: SegmentService,
    private readonly authService: AuthenticationService,
    private readonly translationService: TranslationService,
  ) {
    this.loading = false;
    // Buffer router events every 2 seconds, to reduce loading screen jitter
    this.routerSub = this.router.events.pipe(bufferTime(2000)).subscribe(events => {
      if (events.length === 0) {
        return;
      }

      const event = events[events.length - 1];
      if (event instanceof NavigationStart) {
        this.loading = true;
      }
      if (event instanceof NavigationEnd) {
        this.loading = false;
      }
      // Set loading state to false in both of the below events to hide the spinner in case a request fails
      if (event instanceof NavigationCancel) {
        this.loading = false;
      }
      if (event instanceof NavigationError) {
        this.loading = false;
        const err = event.error;
        if (err && typeof err === 'object' && err.status === 403) {
          this.error = 'You do not have permission to access this page.';
        } else if (err && typeof err === 'object' && err.message) {
          this.error = err.message;
        } else {
          this.error = String(err);
        }
      }
    });

    this.segmentRouterSub = router.events.pipe(distinctUntilChanged((previous: any, current: any) => {
      if (current instanceof NavigationEnd) {
        return previous.url === current.url;
      }
      return true;
    })).subscribe((nav: NavigationEnd) => {
      if (nav && !environment.nt) {
        segment.page(nav.url);
      }
    });

    this.translationService.getCurrentLang().subscribe(lang => {
      this.currentLang = lang;
    });
  }

  ngOnInit(): void {
    this.route.data.subscribe(data => {
      this.error = data.error ? data.error : '';
    });
  }

  isAllow(acl:string):boolean {
    if(this.authService.username === 'admin' || acl === '') return true
    const userAcl = this.authService.acl[acl]
    return userAcl && userAcl.read === true
  }

  getSessionRole(): UserRole {
    return this.authService.sessionRole;
  }

  getUsername(): string {
    return this.authService.username;
  }

  isMfaEnabled(): boolean {
    return !this.authService.session?.mfa_code;
  }

  logout(): void {
    this.authService.logout().subscribe(() => {
      this.router.navigate(['/login']);
    });
  }

  ngOnDestroy(): void {
    this.segmentRouterSub.unsubscribe();
    this.routerSub.unsubscribe();
  }

  onSidebarNavChange(changeEvent: NgbNavChangeEvent): void {}

  switchLanguage(lang: string): void {
    this.translationService.setLanguage(lang);
  }

  toggleUserMenu(): void {
    this.userMenuOpen = !this.userMenuOpen;
  }
}

@Injectable({providedIn: 'root'})
export class PageviewGuard implements CanActivate, CanActivateChild {
  constructor(private readonly authService: AuthenticationService, private readonly router: Router, private readonly globals: Globals) {}

  canActivate(next: ActivatedRouteSnapshot, state: RouterStateSnapshot): boolean {
    return true;
  }

  canActivateChild(next: ActivatedRouteSnapshot, state: RouterStateSnapshot): boolean {
    const path = next.url[0]?.path;

    // Legacy role-based check
    const role = this.globals.restrictedPages.get(path);
    if (role !== null && role < this.authService.sessionRole) {
      const _ = this.router.navigate(['/']);
      return false;
    }

    // ACL-based check: skip for admin user
    const minRole: string = next.data?.minRole;
    if (minRole && this.authService.username !== 'admin') {
      const userAcl = this.authService.acl?.[minRole];
      if (!userAcl || !userAcl.read) {
        const _ = this.router.navigate(['/status']);
        return false;
      }
    }

    return true;
  }
}
